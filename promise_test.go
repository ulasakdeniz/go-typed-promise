package promise

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

func TestPromise_Await(t *testing.T) {
	type fields struct {
		ctx     context.Context
		runTask func() (string, error)
	}

	tests := []struct {
		name          string
		fields        fields
		want          interface{}
		expectedError error
	}{
		{
			name: "return the value",
			fields: fields{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "test", nil
				},
			},
			want: "test",
		},
		{
			name: "return the error",
			fields: fields{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "", fmt.Errorf("test error")
				},
			},
			expectedError: fmt.Errorf("test error"),
		},
		{
			name: "return the error when context is timeout",
			fields: fields{
				ctx: timeoutContext(100 * time.Millisecond),
				runTask: func() (string, error) {
					time.Sleep(200 * time.Millisecond)
					return "test", nil
				},
			},
			expectedError: fmt.Errorf("context error while waiting for promise: context canceled"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			p, err := New(tt.fields.ctx, tt.fields.runTask)
			assert.NoError(t, err)

			p.execute()

			got, err := p.Await()
			if tt.expectedError == nil {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
				assert.Equal(t, true, p.isCompleted.Load())
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError, err)

				assert.Equal(t, true, p.isCompleted.Load())
			}

			assert.Equal(t, true, p.executed.Load())
		})
	}
}

func TestPromise_Await_Concurrent(t *testing.T) {
	p, err := New(context.TODO(), func() (int, error) {
		time.Sleep(1 * time.Second)
		return 5, nil
	})
	assert.NoError(t, err)

	res1Chan := make(chan int)
	res2Chan := make(chan int)
	res3Chan := make(chan int)

	job := func(c chan<- int) {
		res, err := p.Await()
		assert.NoError(t, err)
		c <- res
	}

	go job(res1Chan)
	go job(res2Chan)
	go job(res3Chan)

	assert.Equal(t, 5, <-res2Chan)
	assert.Equal(t, 5, <-res1Chan)
	assert.Equal(t, 5, <-res3Chan)
}

func TestPromise_Await_Concurrent_With_Error(t *testing.T) {
	err := fmt.Errorf("test error")

	p, newErr := New(context.TODO(), func() (int, error) {
		time.Sleep(1 * time.Second)
		return 0, err
	})
	assert.NoError(t, newErr)

	err1Chan := make(chan error)
	err2Chan := make(chan error)
	err3Chan := make(chan error)

	job := func(c chan<- error) {
		_, resultErr := p.Await()
		assert.Error(t, resultErr)
		c <- resultErr
	}

	go job(err1Chan)
	go job(err2Chan)
	go job(err3Chan)

	assert.Equal(t, err, <-err2Chan)
	assert.Equal(t, err, <-err1Chan)
	assert.Equal(t, err, <-err3Chan)
}

func TestNew(t *testing.T) {
	got, err := New(context.TODO(), func() (int, error) {
		return 5, nil
	})
	assert.NoError(t, err)
	assert.NotNil(t, got)
}

func TestNew_NilFunction(t *testing.T) {
	got, err := New(context.TODO(), (func() (int, error))(nil))
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestNew_NilContext(t *testing.T) {
	got, err := New(nil, func() (int, error) {
		return 5, nil
	})
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestCompleted(t *testing.T) {
	p := Completed(5)
	assert.Equal(t, true, p.isCompleted.Load())
	assert.Equal(t, true, p.executed.Load())
	await, err := p.Await()
	assert.NoError(t, err)
	assert.Equal(t, 5, await)
	assert.Equal(t, 5, p.result)
	assert.Equal(t, nil, p.errResult)
}

func TestFailed(t *testing.T) {
	p := Failed[int](fmt.Errorf("test error"))
	assert.Equal(t, true, p.isCompleted.Load())
	assert.Equal(t, true, p.executed.Load())
	assert.Equal(t, 0, p.result)
	assert.Equal(t, fmt.Errorf("test error"), p.errResult)
	// await
	_, err := p.Await()
	assert.Equal(t, "test error", err.Error())
}

func TestPromise_OnComplete(t *testing.T) {
	type fields[T any] struct {
		ctx          context.Context
		runTask      func() (T, error)
		isSuccess    bool
		taskDuration time.Duration
	}

	tests := []struct {
		name   string
		fields fields[string]
	}{
		{
			name: "on complete",
			fields: fields[string]{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "test", nil
				},
				isSuccess:    true,
				taskDuration: 100 * time.Millisecond,
			},
		},
		{
			name: "on complete with time taking task",
			fields: fields[string]{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(1 * time.Second)
					return "test", nil
				},
				isSuccess:    true,
				taskDuration: 1 * time.Second,
			},
		},
		{
			name: "on complete with error",
			fields: fields[string]{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "", fmt.Errorf("test error")
				},
				isSuccess:    false,
				taskDuration: 100 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.fields.ctx, tt.fields.runTask)
			assert.NoError(t, err)

			isCallbackCalled := &atomic.Bool{}

			success := func(isSuccess bool) func(string) {
				callback := func(result string) {
					if isSuccess {
						assert.Equal(t, "test", result)
						assert.NoError(t, err)
						isCallbackCalled.Store(true)
					}
				}
				return callback
			}(tt.fields.isSuccess)

			failure := func(isSuccess bool) func(error) {
				callback := func(err error) {
					if isSuccess {
						t.Errorf("should not be called")
					}

					if !isSuccess {
						isCallbackCalled.Store(true)
					}
				}
				return callback
			}(tt.fields.isSuccess)

			p.OnComplete(success, failure)

			// wait for task to complete
			time.Sleep(tt.fields.taskDuration * 3)
			assert.True(t, isCallbackCalled.Load(), "correct callback should be called")
		})
	}
}

func TestPromise_PipeTo(t *testing.T) {
	expectedError := fmt.Errorf("test error")

	type fields struct {
		ctx          context.Context
		runTask      func() (string, error)
		isSuccess    bool
		taskDuration time.Duration
	}

	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "pipe",
			fields: fields{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "test", nil
				},
				isSuccess:    true,
				taskDuration: 100 * time.Millisecond,
			},
		},
		{
			name: "pipe with error",
			fields: fields{
				ctx: context.TODO(),
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "", expectedError
				},
				isSuccess:    false,
				taskDuration: 100 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.fields.ctx, tt.fields.runTask)
			assert.NoError(t, err)

			successChan := make(chan string)
			failureChan := make(chan error)

			p.ToChannel(successChan, failureChan)

			r, err := p.Await()

			if tt.fields.isSuccess {
				assert.Equal(t, "test", r)
				assert.NoError(t, err)
				assert.Equal(t, "test", <-successChan)
			} else {
				assert.Error(t, err)
				assert.Equal(t, expectedError, <-failureChan)
			}
		})
	}
}

func Test_FromPromise(t *testing.T) {
	type fields struct {
		runTask      func() (string, error)
		isSuccess    bool
		taskDuration time.Duration
		result       int
	}

	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "map",
			fields: fields{
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "test", nil
				},
				isSuccess:    true,
				result:       4,
				taskDuration: 100 * time.Millisecond,
			},
		},
		{
			name: "map with error",
			fields: fields{
				runTask: func() (string, error) {
					time.Sleep(100 * time.Millisecond)
					return "", fmt.Errorf("test error")
				},
				isSuccess:    false,
				taskDuration: 100 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(context.TODO(), tt.fields.runTask)
			assert.NoError(t, err)

			p2, err := FromPromise(p, func(result string, err error) (int, error) {
				if err != nil {
					return 0, err
				}
				return len(result), nil
			})
			assert.NoError(t, err)
			p2Result, err := p2.Await()
			if tt.fields.isSuccess {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tt.fields.result, p2Result)
		})
	}
}

func TestAll(t *testing.T) {
	task := func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "test", nil
	}

	p1, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p2, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p3, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p4, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p5, err := New(context.TODO(), task)
	assert.NoError(t, err)

	all, err := All(context.TODO(), p1, p2, p3, p4, p5)
	assert.NoError(t, err)

	results, err := all.Await()
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 5, len(results))

	for _, r := range results {
		assert.Equal(t, "test", r)
	}
}

func TestAllWithErrors(t *testing.T) {
	expectedErr := fmt.Errorf("test error")
	task := func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "test", nil
	}

	taskWithError := func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "", expectedErr
	}

	taskWithErrorLate := func() (string, error) {
		time.Sleep(200 * time.Millisecond)
		return "", fmt.Errorf("some error")
	}

	p1, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p2, err := New(context.TODO(), task)
	assert.NoError(t, err)

	p3, err := New(context.TODO(), taskWithError)
	assert.NoError(t, err)

	p4, err := New(context.TODO(), taskWithErrorLate)
	assert.NoError(t, err)

	p5, err := New(context.TODO(), taskWithErrorLate)
	assert.NoError(t, err)

	all, err := All(context.TODO(), p1, p2, p3, p4, p5)
	assert.NoError(t, err)

	results, err := all.Await()
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 0, len(results))
}

func TestAny(t *testing.T) {
	task := func(duration time.Duration, id int) func() (string, error) {
		return func() (string, error) {
			time.Sleep(duration)
			return fmt.Sprintf("test-%d", id), nil
		}
	}

	p1, err := New(context.TODO(), task(100*time.Millisecond, 100))
	assert.NoError(t, err)

	p2, err := New(context.TODO(), task(200*time.Millisecond, 200))
	assert.NoError(t, err)

	p3, err := New(context.TODO(), task(300*time.Millisecond, 300))
	assert.NoError(t, err)

	p, err := Any(context.TODO(), p1, p2, p3)
	assert.NoError(t, err)

	result, err := p.Await()
	assert.NoError(t, err)
	assert.Equal(t, "test-100", result)
}

func TestAnyWithErrors(t *testing.T) {
	expectedErr := fmt.Errorf("test error")
	task := func(duration time.Duration, id int) func() (string, error) {
		return func() (string, error) {
			time.Sleep(duration)
			return fmt.Sprintf("test-%d", id), nil
		}
	}

	taskWithError := func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "", expectedErr
	}

	p1, err := New(context.TODO(), task(100*time.Millisecond, 100))
	assert.NoError(t, err)

	p2, err := New(context.TODO(), task(200*time.Millisecond, 200))
	assert.NoError(t, err)

	p3, err := New(context.TODO(), taskWithError)
	assert.NoError(t, err)

	p, err := Any(context.TODO(), p1, p2, p3)
	assert.NoError(t, err)

	result, err := p.Await()
	assert.NoError(t, err)
	assert.Equal(t, "test-100", result)
}

func TestAnyWithOnlyErrors(t *testing.T) {
	taskWithError := func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "", fmt.Errorf("test error")
	}

	p1, err := New(context.TODO(), taskWithError)
	assert.NoError(t, err)

	p2, err := New(context.TODO(), taskWithError)
	assert.NoError(t, err)

	p3, err := New(context.TODO(), taskWithError)
	assert.NoError(t, err)

	p, err := Any(context.TODO(), p1, p2, p3)
	assert.NoError(t, err)

	_, err = p.Await()
	assert.Equal(t, "all promises failed", err.Error())
}

func timeoutContext(d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.TODO(), d)
	defer cancel()
	return ctx
}
