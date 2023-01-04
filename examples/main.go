package main

import (
	"context"
	"fmt"
	promise "github.com/ulasakdeniz/go-typed-promise"
	"io"
	"net/http"
	"time"
)

func main() {
	// IP EXAMPLE
	ipPromise, _ := promise.New(context.TODO(), func() (string, error) {
		res, _ := http.Get("https://api.ipify.org")
		ip, _ := io.ReadAll(res.Body)
		return string(ip), nil
	})

	ip, err := ipPromise.Await()
	if err != nil {
		fmt.Println("error", err)
	}

	fmt.Println("ip", ip)

	// CHANNEL
	stringChannel := make(chan string)
	errChannel := make(chan error)
	p, _ := promise.New(context.TODO(), func() (string, error) {
		time.Sleep(1 * time.Second)
		return "Hello", nil
	})

	p.ToChannel(stringChannel, errChannel)

	result := <-stringChannel
	fmt.Println("result", result)
	// Output: result Hello

	// TIMEOUT
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	timeoutPromise, _ := promise.New(ctx, func() (string, error) {
		time.Sleep(2 * time.Second)
		return "Hello", nil
	})

	_, timeoutErr := timeoutPromise.Await()
	fmt.Println(timeoutErr)
	// Output: context error while waiting for promise: context deadline exceeded
}
