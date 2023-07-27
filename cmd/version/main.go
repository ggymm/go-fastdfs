package main

import (
	"fmt"
	"oss/server"
)

func main() {
	fmt.Printf("Version   : %s\n", server.VERSION)
	fmt.Printf("GO_VERSION: %s\n", server.GO_VERSION)
	fmt.Printf("GIT_COMMIT: %s\n", server.GIT_VERSION)
	fmt.Printf("BUILD_TIME: %s\n", server.BUILD_TIME)
}
