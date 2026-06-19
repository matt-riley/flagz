module github.com/matt-riley/flagz/clients/go

go 1.26.1

require (
	github.com/matt-riley/flagz v1.13.3
	google.golang.org/grpc v1.81.1
)

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/matt-riley/flagz => ../..
