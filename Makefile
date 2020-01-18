APP=mqtt-mirror
APP_EXECUTABLE="./out/$(APP)"

setup:
	go get golang.org/x/lint/golint
	go get github.com/mattn/goveralls

compile:
	mkdir -p out
	go build -o $(APP_EXECUTABLE) main.go

lint:
	@golint ./... | { grep -vwE "exported (var|function|method|type|const) \S+ should have comment" || true; }

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
