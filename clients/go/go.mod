module github.com/matt-riley/flagz/clients/go

go 1.26.6

require (
	github.com/matt-riley/flagz v1.13.3
	google.golang.org/grpc v1.83.2
)

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace github.com/matt-riley/flagz => ../..
