call del /q dist\\fileserver
call del /q dist\\fileserver-arm

call cd cmd/server
call set GOOS=linux
call set GOARCH=amd64
call go build -ldflags "-s -w" -o ../../dist/fileserver

call set GOOS=linux
call set GOARCH=arm64
call go build -ldflags "-s -w" -o ../../dist/fileserver-arm

@pause