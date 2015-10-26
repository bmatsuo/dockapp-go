.PHONY: bin

bin: 
	mkdir -p bin
	GOBIN=bin go install ./cmd/...

clean:
	rm -f bin/*

install:
	go install ./cmd/...
