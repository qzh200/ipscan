module ipscan

go 1.27.3

require (
	github.com/egoist/mygo v0.8.4-0.20261108082913-9233663736ca
	github.com/oneo-me/onevra/source v0.2026.19-0.20261107095720-3af36d2c8d0b
	golang.org/x/crypto v0.58.0
	golang.org/x/text v0.43.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ebitengine/purego v0.11.2 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace gopkg.in/yaml.v3 => ./third_party/yaml
