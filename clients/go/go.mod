module github.com/matt-riley/flagz/clients/go

go 1.26.4

require (
	github.com/matt-riley/flagz v1.13.3
	google.golang.org/grpc v1.82.1
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/matt-riley/flagz => ../..
