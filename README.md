# go-typed-promise

A type-safe generic promise library for Go 1.19.

## Installation

```bash
go get github.com/ulasakdeniz/go-typed-promise
```

## Usage

- [Creating a Promise](#creating-a-promise)
- [Using Await](#using-await)
- [Creating a promise with a timeout](#creating-a-promise-with-a-timeout)
- [Chaining Promises](#chaining-promises)
- [Using Promise.All](#using-promiseall)
- [Using Promise.Any](#using-promiseany)
- [Callbacks: OnComplete, OnSuccess, OnFailure](#callbacks-oncomplete-onsuccess-onfailure)
- [Piping promises to channels](#piping-promises-to-channels)

*Errors are ignored in the examples below for simplicity.*

### Creating a promise

Create a promise with `New` function.
It is a generic function that takes a `context.Context` and a `func() (T, error)` as parameters.
The function returns a `*Promise[T]`.

```go
package main

import (
	"context"
	"fmt"
	"time"
	"github.com/ulasakdeniz/go-typed-promise"
)

func main() {
	// the type of p is Promise[string]
	p, err := promise.New(context.TODO(), func() (string, error) {
		// simulate a long running task that makes a network call
		time.Sleep(1 * time.Second)
		return "Hello", nil
	})

	// err is not nil if an error occurs while creating the promise
	if err != nil {
		// handle error
	}

	// await the result
	result, err := p.Await()
	if err != nil {
		// handle error
	}

	fmt.Println(result) // prints "Hello"
}

```

### Using `Await`

`Await` function returns the result of the promise.
Calling `Await` multiple times will return the same result. The promise will be resolved only once.
Note that `Await` is a blocking call. It waits until the promise is resolved.

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ulasakdeniz/go-typed-promise"
)

func main() {
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
	// Output: ip <YOUR_IP>
}
```

### Creating a promise with a timeout

A `context.Context` is used to create a promise with a timeout.
If the promise is not resolved within the timeout, the promise will be rejected with an error.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/ulasakdeniz/go-typed-promise"
)

func main() {
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
```

### Chaining promises

Promises can be chained with `Map` function. It takes a `func(T) (T, error)` as a parameter.
The function returns a `Promise[T]` which is a new promise created from the result of the previous promise.

```go
package main

import (
    "context"
    "fmt"
	"io"
    "net/http"

    "github.com/ulasakdeniz/go-typed-promise"
)

func main() {
	tokenPromise, _ := promise.New(context.TODO(), func() (string, error) {
		res, _ := http.Get("https://token-api")
		token, _ := io.ReadAll(res.Body)
		return string(token), nil
	})

	// Get user, Map returns a new promise that is using tokenPromise to get token
	userPromise := tokenPromise.Map(func(token string) (string, error) {
		res, _ := http.Get("https://user-api?token=" + token)
		user, _ := io.ReadAll(res.Body)
		return string(user), nil
	})

	user, err := userPromise.Await()
	if err != nil {
		fmt.Println("error", err)
	}

	fmt.Println("user", user)
}
```

### Using `Promise.All`

`Promise.All` takes a number of promises and returns a promise that resolves when all the promises in the slice are resolved.
The result of the promise is a slice of the results of the promises.

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ulasakdeniz/go-typed-promise"
)

func main() {
	// ignoring errors for simplicity
	promise1, _ := promise.New(context.TODO(), func() (string, error) {
		res, _ := http.Get("https://data-api/1") // returns "data1"
		data, _ := io.ReadAll(res.Body)
		return string(data), nil
	})

	promise2, _ := promise.New(context.TODO(), func() (string, error) {
		res, _ := http.Get("https://data-api/2") // returns "data2"
		data, _ := io.ReadAll(res.Body)
		return string(data), nil
	})

	// Get data in parallel
	allPromise, _ := promise.All(context.TODO(), promise1, promise2)

	// Await the result
	result, _ := allPromise.Await()

	fmt.Println("result", result) // prints ["data1", "data2"]
}
```

### Using `Promise.Any`

`Promise.Any` takes a number of promises and returns a promise that resolves when a promise successfully completes.
The result of the promise is the result of the first promise that completes.
If all the promises fail, the promise will have error.

### Callbacks: `OnComplete`, `OnSuccess`, `OnFailure`

`OnComplete` is called when the promise is resolved or rejected.
`OnSuccess` is called when the promise is resolved.
`OnFailure` is called when the promise is rejected.

### Piping promises to channels

`ToChannel` function pipes the result of the promise to a channel.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/ulasakdeniz/go-typed-promise"
)

func main() {
	stringChannel := make(chan string)
	errChannel := make(chan error)
	p, _ := promise.New(context.TODO(), func() (string, error) {
		time.Sleep(1 * time.Second)
		return "Hello", nil
	})

	p.ToChannel(stringChannel, errChannel)

    fmt.Println(<-stringChannel) // Output: "Hello"
}
```

If the promise results in error, the error will be sent to the error channel.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/ulasakdeniz/go-typed-promise"
)

func main() {
    stringChannel := make(chan string)
    errChannel := make(chan error)
    p, _ := promise.New(context.TODO(), func() (string, error) {
        time.Sleep(1 * time.Second)
        return "", fmt.Errorf("error")
    })

    p.ToChannel(stringChannel, errChannel)

    fmt.Println(<-errChannel)
	// Output: "error"
}
```
