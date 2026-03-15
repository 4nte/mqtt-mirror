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
	go test -race ./... -covermode=atomic -coverprofile=profile.cov

test-e2e:
	go test -tags=e2e ./e2e/ -v -timeout 10m -count=1

docker-image:
	docker build -t ${USER}/mqtt-mirror:latest -f build/Dockerfile .

docker-image-tag:
	docker build -t antegulin/mqtt-mirror:$DRONE_TAG -f build/Dockerfile .
