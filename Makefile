.PHONY: build run clean tidy check testbot

BINARY=l1jgo

build:
	go build -o bin/$(BINARY) ./cmd/l1jgo

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin/

tidy:
	go mod tidy

# 快速驗證：編譯 + 靜態分析
check:
	go build ./...
	go vet ./...

# 編譯測試客戶端
testbot:
	go build -o bin/testbot ./cmd/testbot
