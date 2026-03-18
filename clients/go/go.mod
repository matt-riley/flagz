module github.com/matt-riley/flagz/clients/go

go 1.26.1

require (
	github.com/matt-riley/flagz v1.13.2
	google.golang.org/grpc v1.79.3
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/matt-riley/flagz => ../..
