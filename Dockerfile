FROM golang:1.25-alpine

WORKDIR /app

COPY . .
RUN go mod download && go build -o seatbooking ./cmd/seatbooking

EXPOSE 8080
CMD ["sh", "./docker-entrypoint.sh"]