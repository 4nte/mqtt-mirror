APP=mqtt-mirror
APP_EXECUTABLE="./out/$(APP)"

setup:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

compile:
	mkdir -p out
	go build -ldflags="-X github.com/4nte/mqtt-mirror/cmd.version=dev" -o $(APP_EXECUTABLE) main.go

lint:
	golangci-lint run

format:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./... -covermode=count -coverprofile=profile.cov

docker-image:
	docker build -t ${USER}/mqtt-mirror:latest -f build/Dockerfile .

docker-image-tag:
	docker build -t antegulin/mqtt-mirror:$DRONE_TAG -f build/Dockerfile .
