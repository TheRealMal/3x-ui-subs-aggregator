.PHONY: swagger install-swag

install-swag:
	go install github.com/swaggo/swag/cmd/swag@latest

swagger: install-swag
	swag init -g cmd/subs-aggregator/main.go -o docs --outputTypes yaml
