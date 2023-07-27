call del /q dist\\fileserver.exe

call cd cmd/server
call go build -ldflags "-s -w" -o ../../dist/fileserver.exe

@pause