package promise

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	ContextError = "context error while waiting for promise: %s"
)

// Promise is a wrapper for a task that will be executed asynchronously.
type Promise[T any] struct {
	ctx         context.Context
	runTask     func() (T, error)
	executed    *atomic.Bool
	isCompleted *atomic.Bool
	subscribers subscribers[T]
	result      T
	errResult   error
	valueChan   chan T
	errChan     chan error
}

type subscriber[T any] struct {
	valueChan chan T
	errChan   chan error
}

type subscriberSet[T any] map[subscriber[T]]struct{}

type subscribers[T any] struct {
	set  subscriberSet[T]
	lock *sync.RWMutex
}

func (s *subscribers[T]) add(subscriber subscriber[T]) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.set[subscriber] = struct{}{}
}

func (s *subscribers[T]) get() subscriberSet[T] {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.set
}

func (p *Promise[T]) subscribe(s subscriber[T]) {
	if p.isCompleted.Load() {
		p.notify(s)
	} else {
		p.subscribers.add(s)
	}
}

func (p *Promise[T]) notify(s subscriber[T]) {
	defer close(s.valueChan)
	defer close(s.errChan)

	if p.errResult != nil {
		s.errChan <- p.errResult
	} else {
		s.valueChan <- p.result
	}
}

func (p *Promise[T]) broadcast() {
	for s := range p.subscribers.get() {
		go p.notify(s)
	}
}

func (p *Promise[T]) collect() {
	select {
	case <-p.ctx.Done():
		err := fmt.Errorf(ContextError, p.ctx.Err())
		p.errResult = err
	case value := <-p.valueChan:
		p.result = value
	case err := <-p.errChan:
		p.errResult = err
	}

	p.isCompleted.Store(true)
	p.broadcast()
}

func (p *Promise[T]) execute() {
	if p.executed.Load() {
		return
	}
	p.executed.Store(true)

	go p.collect()

	go func() {
		defer close(p.valueChan)
		defer close(p.errChan)

		value, err := p.runTask()
		if err != nil {
			p.errChan <- err
		} else {
			p.valueChan <- value
		}
	}()
}

// IsCompleted returns true if the promise is completed.
func (p *Promise[T]) IsCompleted() bool {
	return p.isCompleted.Load()
}

// Map maps the result of the promise to a new value.
func (p *Promise[T]) Map(f func(T, error) (T, error)) *Promise[T] {
	return create(p.ctx, func() (T, error) {
		value, err := p.Await()
		return f(value, err)
	})
}

// OnComplete is a callback function that is called when the promise is completed.
func (p *Promise[T]) OnComplete(success func(T), failure func(error)) {
	go func() {
		value, err := p.Await()
		if err != nil {
			failure(err)
		}
		success(value)
	}()
}

// OnSuccess is a callback function that is called when the promise is completed successfully.
func (p *Promise[T]) OnSuccess(success func(T)) {
	p.OnComplete(success, func(error) {})
}

// OnFailure is a callback function that is called when the promise is failed.
func (p *Promise[T]) OnFailure(failure func(error)) {
	p.OnComplete(func(T) {}, failure)
}

// ToChannel sends the result or error of the promise to the given channels.
func (p *Promise[T]) ToChannel(success chan<- T, failure chan<- error) {
	p.OnComplete(func(value T) {
		success <- value
	}, func(err error) {
		failure <- err
	})
}

// Await waits for the promise to complete and returns the result or error.
func (p *Promise[T]) Await() (T, error) {
	if p.isCompleted.Load() {
		if p.errResult != nil {
			var t T
			return t, p.errResult
		}
		return p.result, nil
	}

	s := subscriber[T]{
		valueChan: make(chan T),
		errChan:   make(chan error),
	}

	p.subscribe(s)

	select {
	case value := <-s.valueChan:
		return value, nil
	case err := <-s.errChan:
		var result T
		return result, err
	}
}

// Completed returns a promise that is already completed with the given value.
func Completed[T any](value T) *Promise[T] {
	var executed atomic.Bool
	var isCompleted atomic.Bool
	executed.Store(true)
	isCompleted.Store(true)
	return &Promise[T]{
		executed:    &executed,
		isCompleted: &isCompleted,
		result:      value,
	}
}

// Failed returns a promise that is already completed with the given error.
func Failed[T any](err error) *Promise[T] {
	var executed atomic.Bool
	var isCompleted atomic.Bool
	executed.Store(true)
	isCompleted.Store(true)
	return &Promise[T]{
		executed:    &executed,
		isCompleted: &isCompleted,
		errResult:   err,
	}
}

// New returns a new Promise[T] for the given task.
func New[T any](ctx context.Context, task func() (T, error)) (*Promise[T], error) {
	if task == nil {
		err := fmt.Errorf("no function provided")
		return nil, err
	}

	if ctx == nil {
		err := fmt.Errorf("no context provided")
		return nil, err
	}

	return create(ctx, task), nil
}

func create[T any](ctx context.Context, task func() (T, error)) *Promise[T] {
	valueChan := make(chan T)
	errChan := make(chan error)

	p := &Promise[T]{
		ctx:         ctx,
		runTask:     task,
		executed:    &atomic.Bool{},
		isCompleted: &atomic.Bool{},
		subscribers: subscribers[T]{
			set:  make(subscriberSet[T]),
			lock: &sync.RWMutex{},
		},
		valueChan: valueChan,
		errChan:   errChan,
	}

	p.execute()

	return p
}

// singleResult is a type to store a single result from a promise while collecting results
type singleResult[T any] struct {
	index int
	value T
}

// All collects all results from a list of promises and returns them in a slice of Promise.
func All[T any](ctx context.Context, promises ...*Promise[T]) (*Promise[[]T], error) {
	numOfPromises := len(promises)

	if numOfPromises == 0 {
		err := fmt.Errorf("no promises provided")
		return nil, err
	}

	p, err := New[[]T](ctx, func() ([]T, error) {
		valueChan := make(chan singleResult[T], numOfPromises)
		errChan := make(chan error)

		for i, p := range promises {
			go func(index int, p *Promise[T]) {
				value, err := p.Await()
				if err != nil {
					errChan <- err
					return
				}

				valueChan <- singleResult[T]{index: index, value: value}
			}(i, p)
		}

		results := make([]T, numOfPromises)

		for i := 0; i < numOfPromises; i++ {
			select {
			case value := <-valueChan:
				results[value.index] = value.value
			case err := <-errChan:
				return nil, err
			}
		}

		return results, nil
	})

	if err != nil {
		return nil, err
	}

	return p, nil
}

// Any returns a promise that is completed with the first successful result of the given promises.
// If all promises fail, the returned promise is failed with an error.
func Any[T any](ctx context.Context, promises ...*Promise[T]) (*Promise[T], error) {
	if len(promises) == 0 {
		err := fmt.Errorf("no promises provided")
		return nil, err
	}

	p, err := New[T](ctx, func() (T, error) {
		valueChan := make(chan T)
		errChan := make(chan error)

		for _, p := range promises {
			go func(p *Promise[T]) {
				value, err := p.Await()
				if err != nil {
					errChan <- err
					return
				}

				valueChan <- value
			}(p)
		}

		failedPromises := 0
		for failedPromises < len(promises) {
			select {
			case value := <-valueChan:
				return value, nil
			case <-errChan:
				failedPromises++
			}
		}

		var r T
		return r, fmt.Errorf("all promises failed")
	})

	if err != nil {
		return nil, err
	}

	return p, nil
}

// FromPromise maps the result of the given promise to a new promise.
func FromPromise[T any, R any](promise *Promise[T], mapper func(T, error) (R, error)) (*Promise[R], error) {
	newPromise, err := New(promise.ctx, func() (R, error) {
		value, err := promise.Await()
		return mapper(value, err)
	})
	if err != nil {
		return nil, err
	}

	return newPromise, nil
}
