# go-typed-promise

An experimental type-safe generic promise library for Go 1.19.

## Why?

![Why](https://media.giphy.com/media/s239QJIh56sRW/giphy.gif)

Go has powerful concurrency tools in the form of channels and goroutines.
However, the introduction of generics allows for the creation of abstractions, such as the
Promise pattern, on top of these basic concurrency tools.

Promises can be useful when you have a task that you want to run asynchronously and reuse the result of that task.
They can also make it easier to retrieve data from goroutines.
Additionally, if still needed, the result of a promise can be sent to a channel using `ToChannel`.
See [Piping promises to channels](#piping-promises-to-channels) section.

### A use case

Think about this sample task:

- Make a request to a remote server
- Then save the result to a database and publish to a queue at the same time
- Run each operation (http call, saving in db and publishing to queue) in separate goroutines.
- If any of these operations fail, terminate the task. 
- Implement a timeout of 1 second for the entire task.

These network operations are made in a goroutine to allow for the possibility of running this flow concurrently multiple
times in the future.

[![](https://mermaid.ink/img/pako:eNp9UbtuwzAM_BVCQIAUiAdn1BCgjtdM7VSrA2PRD8CSXD1SBHH-vZILN01QVFrI04l3JC-sNpIYZ81gPusOrYfXQmgXjq3FsYNe0haq7bvQz9vqbIIFR_ZENgL7av0RKNBTjMtqLdHjEd2ckpb3NXKo8lQjf6hRVJaU8XRD5q8PvCzbTS15SBJTkaxAgsZwHHrXgSLnsKVp__Pi8EQTlEKnu1rFOtGprqnsMVpSM5bOL5Fst7uzwmFR_JMd6UvDHJLcv9R5ThweDLMNU2QV9jLO_yI0gGC-I0WC8RhKajAMXjChr5GKwZuXs64Z9zbQhoUxKi4dMd7g4CI6on4z5paT7L2xh-8dz6u-fgEeRa3U?type=png)](https://mermaid.live/edit#pako:eNp9UbtuwzAM_BVCQIAUiAdn1BCgjtdM7VSrA2PRD8CSXD1SBHH-vZILN01QVFrI04l3JC-sNpIYZ81gPusOrYfXQmgXjq3FsYNe0haq7bvQz9vqbIIFR_ZENgL7av0RKNBTjMtqLdHjEd2ckpb3NXKo8lQjf6hRVJaU8XRD5q8PvCzbTS15SBJTkaxAgsZwHHrXgSLnsKVp__Pi8EQTlEKnu1rFOtGprqnsMVpSM5bOL5Fst7uzwmFR_JMd6UvDHJLcv9R5ThweDLMNU2QV9jLO_yI0gGC-I0WC8RhKajAMXjChr5GKwZuXs64Z9zbQhoUxKi4dMd7g4CI6on4z5paT7L2xh-8dz6u-fgEeRa3U)

### Implementing with errgroup and promise

We will see the implementation of the sample task using both the errgroup and promise packages.
While [golang.org/x/sync/errgroup](https://pkg.go.dev/golang.org/x/sync@v0.1.0/errgroup) package is well-suited for tasks
of this nature, the promise implementation may offer a simpler and more straightforward approach.

First, let's set the stage with mock functions for our tasks (uncomment the `time.Sleep` to see the timeout works):

```go
// make http request
func httpCall() (string, error) {
    //time.Sleep(2 * time.Second)
    return "Hello World", nil
}

// publish message to a queue
func publishMessage(_ string) (string, error) {
    //time.Sleep(2 * time.Second)
    return "success", nil
}

// save to database
func saveToDB(_ string) (string, error) {
    //time.Sleep(2 * time.Second)
    return "success", nil
}
```

<details>
<summary><b>Click to see implementation with errgroup</b></summary>

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	g, ctx := errgroup.WithContext(timeoutCtx)

	httpResChan := make(chan string, 2)
	publishChan := make(chan string)
	saveChan := make(chan string)

	// make http request
	g.Go(func() error {
		defer close(httpResChan)

		internalResChan := make(chan string)
		internalErrChan := make(chan error)

		// make http request and send to internal channels so that it can timeout and if result is available sent to other channels
		go func() {
			defer close(internalResChan)
			defer close(internalErrChan)

			res, err := httpCall()
			if err != nil {
				internalErrChan <- err
				return
			}
			internalResChan <- res
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-internalResChan:
			httpResChan <- res
			httpResChan <- res // broadcast
			return nil
		case err := <-internalErrChan:
			return err
		}
	})

	// use this function to publish message and save to db, they both receive the http result and pretty much do the same thing
	runWithHttpRes := func(out chan<- string, task func(string) (string, error)) {
		// publish message to a queue
		g.Go(func() error {
			defer close(out)

			internalResChan := make(chan string)
			internalErrChan := make(chan error)

			go func() {
				defer close(internalResChan)
				defer close(internalErrChan)

				res, err := task(<-httpResChan)
				if err != nil {
					internalErrChan <- err
					return
				}
				internalResChan <- res
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case res := <-internalResChan:
				out <- res
				return nil
			case err := <-internalErrChan:
				return err
			}
		})
	}

	// publish message to a queue
	runWithHttpRes(publishChan, publishMessage)
	// save to database
	runWithHttpRes(saveChan, saveToDB)

	results := make([]string, 2)

	// collect results
	g.Go(func() error {
		var counter int32
		for counter < 2 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case publishRes := <-publishChan:
				counter++
				results[0] = publishRes
				publishChan = nil
			case saveRes := <-saveChan:
				counter++
				results[1] = saveRes
				saveChan = nil
			}
		}
		return nil
	})

	// wait for all goroutines to finish.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(results)
	}
}
```
</details>

<details>
<summary><b>Click to see implementation using promise</b></summary>

```go
func main() {
    ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
    defer cancel()

    httpCallPromise, _ := promise.New(ctx, httpCall)

    publishMessagePromise := httpCallPromise.Map(func (data string, httpErr error) (string, error) {
        if httpErr != nil {
            return "", httpErr
        }
        return publishMessage(data)
    })

    saveToDBPromise := httpCallPromise.Map(func (data string, httpErr error) (string, error) {
        if httpErr != nil {
            return "", httpErr
        }
        return saveToDB(data)
    })

    resultPromise, _ := promise.All(ctx, publishMessagePromise, saveToDBPromise)
    result, err := resultPromise.Await()
    if err != nil {
        fmt.Println(err)
    }

    fmt.Println(result)
}

```
</details>

## Installation

It is still experimental and not intended to be used in production.

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

Note: Because of Golang generics limitations, you cannot change the type of the promise with `Map`.
If you want to change the type of the promise, you can use `promise.FromPromise` function.

### Using `Promise.All`

`Promise.All` takes a number of promises and returns a promise that resolves when all the promises in the slice are resolved.
The result of the promise is a slice of the results of the promises.

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
