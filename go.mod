module code.cloudfoundry.org/grootfs

go 1.23.0

toolchain go1.23.6

require (
	code.cloudfoundry.org/commandrunner v0.33.0
	code.cloudfoundry.org/idmapper v0.0.0-20250324182130-24cb87edf5b9
	code.cloudfoundry.org/lager/v3 v3.32.0
	github.com/cloudfoundry/dropsonde v1.1.0
	github.com/cloudfoundry/sonde-go v0.0.0-20250331174251-ed5dffef2bf1
	github.com/containers/image/v5 v5.34.3
	github.com/containers/storage v1.57.2
	github.com/docker/distribution v2.8.3+incompatible
	github.com/docker/docker v28.0.4+incompatible
	github.com/docker/go-units v0.5.0
	github.com/onsi/ginkgo/v2 v2.23.3
	github.com/onsi/gomega v1.36.3
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/opencontainers/runtime-spec v1.2.1
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli/v2 v2.27.6
	github.com/ventu-io/go-shortid v0.0.0-20201117134242-e59966efd125
	golang.org/x/sys v0.31.0
	google.golang.org/protobuf v1.36.6
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/containers/libtrust v0.0.0-20230121012942-c1716e8a8d01 // indirect
	github.com/containers/ocicrypt v1.2.1 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/moby/sys/capability v0.4.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/user v0.3.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/openzipkin/zipkin-go v0.4.3 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/teris-io/shortid v0.0.0-20201117134242-e59966efd125 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/tools v0.31.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace code.cloudfoundry.org/idmapper => ../idmapper
