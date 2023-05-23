// Package dropsonde_unmarshaller provides a tool for unmarshalling Envelopes
// from Protocol Buffer messages.
//
// Use
//
// Instantiate a Marshaller and run it:
//
//		unmarshaller := dropsonde_unmarshaller.NewDropsondeUnMarshaller()
//		inputChan :=  make(chan []byte) // or use a channel provided by some other source
//		outputChan := make(chan *events.Envelope)
//		go unmarshaller.Run(inputChan, outputChan)
//
// The unmarshaller self-instruments, counting the number of messages
// processed and the number of errors. These can be accessed through the Emit
// function on the unmarshaller.
package dropsonde_unmarshaller

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/cloudfoundry/sonde-go/events"
	"google.golang.org/protobuf/proto"
)

var metricNames map[events.Envelope_EventType]string

func init() {
	metricNames = make(map[events.Envelope_EventType]string)
	for eventType, eventName := range events.Envelope_EventType_name {
		r, n := utf8.DecodeRuneInString(eventName)
		modifiedName := string(unicode.ToLower(r)) + eventName[n:]
		metricName := "dropsondeUnmarshaller." + modifiedName + "Received"
		metricNames[events.Envelope_EventType(eventType)] = metricName
	}
}

// A DropsondeUnmarshaller is an self-instrumenting tool for converting Protocol
// Buffer-encoded dropsonde messages to Envelope instances.
type DropsondeUnmarshaller struct {
}

// NewDropsondeUnmarshaller instantiates a DropsondeUnmarshaller.
func NewDropsondeUnmarshaller() *DropsondeUnmarshaller {
	return &DropsondeUnmarshaller{}
}

// Run reads byte slices from inputChan, unmarshalls them to Envelopes, and
// emits the Envelopes onto outputChan. It operates one message at a time, and
// will block if outputChan is not read.
func (u *DropsondeUnmarshaller) Run(inputChan <-chan []byte, outputChan chan<- *events.Envelope) {
	for message := range inputChan {
		envelope, err := u.UnmarshallMessage(message)
		if err != nil {
			continue
		}
		outputChan <- envelope
	}
}

func (u *DropsondeUnmarshaller) UnmarshallMessage(message []byte) (*events.Envelope, error) {
	envelope := &events.Envelope{}
	err := proto.Unmarshal(message, envelope)
	if err != nil {
		metrics.BatchIncrementCounter("dropsondeUnmarshaller.unmarshalErrors")
		return nil, err
	}

	if err := u.incrementReceiveCount(envelope.GetEventType()); err != nil {
		return nil, err
	}

	return envelope, nil
}

func (u *DropsondeUnmarshaller) incrementReceiveCount(eventType events.Envelope_EventType) error {
	var err error
	switch eventType {
	case events.Envelope_LogMessage:
		// LogMessage is a special case. `logMessageReceived` used to be broken out by app ID, and
		// `logMessageTotal` was the sum of all of those.
		metrics.BatchIncrementCounter("dropsondeUnmarshaller.logMessageTotal")
	default:
		metricName := metricNames[eventType]
		if metricName == "" {
			metricName = "dropsondeUnmarshaller.unknownEventTypeReceived"
			err = fmt.Errorf("dropsondeUnmarshaller: received unknown event type %#v", eventType)
		}
		metrics.BatchIncrementCounter(metricName)
	}

	return err
}
