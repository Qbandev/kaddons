.PHONY: build clean

build:
	go build -o kaddons .

clean:
	rm -f kaddons
