clean:
	rm -rf pb/*
pb:
	mkdir pb
gen: pb
	protoc --go_out=pb --go_opt=paths=source_relative --go-grpc_out=pb --go-grpc_opt=paths=source_relative --proto_path=protos protos/*.proto
up: gen
	docker compose --env-file=.env up --build
