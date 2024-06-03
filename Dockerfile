FROM golang:1.22

WORKDIR /usr/src/server

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /usr/local/bin/server ./main.go

CMD server
