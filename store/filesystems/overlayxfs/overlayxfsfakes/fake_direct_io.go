// Code generated by counterfeiter. DO NOT EDIT.
package overlayxfsfakes

import (
	"sync"

	"code.cloudfoundry.org/grootfs/store/filesystems/overlayxfs"
)

type FakeDirectIO struct {
	ConfigureStub        func(string) error
	configureMutex       sync.RWMutex
	configureArgsForCall []struct {
		arg1 string
	}
	configureReturns struct {
		result1 error
	}
	configureReturnsOnCall map[int]struct {
		result1 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeDirectIO) Configure(arg1 string) error {
	fake.configureMutex.Lock()
	ret, specificReturn := fake.configureReturnsOnCall[len(fake.configureArgsForCall)]
	fake.configureArgsForCall = append(fake.configureArgsForCall, struct {
		arg1 string
	}{arg1})
	stub := fake.ConfigureStub
	fakeReturns := fake.configureReturns
	fake.recordInvocation("Configure", []interface{}{arg1})
	fake.configureMutex.Unlock()
	if stub != nil {
		return stub(arg1)
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeDirectIO) ConfigureCallCount() int {
	fake.configureMutex.RLock()
	defer fake.configureMutex.RUnlock()
	return len(fake.configureArgsForCall)
}

func (fake *FakeDirectIO) ConfigureCalls(stub func(string) error) {
	fake.configureMutex.Lock()
	defer fake.configureMutex.Unlock()
	fake.ConfigureStub = stub
}

func (fake *FakeDirectIO) ConfigureArgsForCall(i int) string {
	fake.configureMutex.RLock()
	defer fake.configureMutex.RUnlock()
	argsForCall := fake.configureArgsForCall[i]
	return argsForCall.arg1
}

func (fake *FakeDirectIO) ConfigureReturns(result1 error) {
	fake.configureMutex.Lock()
	defer fake.configureMutex.Unlock()
	fake.ConfigureStub = nil
	fake.configureReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeDirectIO) ConfigureReturnsOnCall(i int, result1 error) {
	fake.configureMutex.Lock()
	defer fake.configureMutex.Unlock()
	fake.ConfigureStub = nil
	if fake.configureReturnsOnCall == nil {
		fake.configureReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.configureReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *FakeDirectIO) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.configureMutex.RLock()
	defer fake.configureMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeDirectIO) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ overlayxfs.DirectIO = new(FakeDirectIO)
