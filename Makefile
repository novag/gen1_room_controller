all:
	export CC_FOR_TARGET=arm-linux-gnueabihf-gcc CC=arm-linux-gnueabihf-gcc; env CGO_ENABLED=1 GOOS=linux GOARCH=arm go build
