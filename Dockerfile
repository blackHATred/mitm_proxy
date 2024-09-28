FROM golang:1.22-alpine

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o main app/main.go

CMD ["./main", "--db=mongodb://mongo:27017", "--proxy=:8000", "--web=:8080"]

EXPOSE 8000
EXPOSE 8080