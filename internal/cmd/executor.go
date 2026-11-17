package cmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type CommandExecutor struct {
	lock           sync.Mutex
	closed         bool
	usedContext    map[context.Context]context.CancelFunc
	openExecutions sync.WaitGroup
}

func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{
		usedContext: map[context.Context]context.CancelFunc{},
	}
}

func (c *CommandExecutor) ExecuteCommand(cmd string, args []string) ([]byte, error) {
	return c.ExecuteCommandWithStdin(cmd, args, context.Background(), nil)
}

func (c *CommandExecutor) ExecuteCommandWithContext(cmd string, args []string, executionContext context.Context) ([]byte, error) {
	return c.ExecuteCommandWithStdin(cmd, args, executionContext, nil)
}

// ExecuteCommandWithStdin runs the command; if stdin is not nil its content
// is piped into the command's standard input.
func (c *CommandExecutor) ExecuteCommandWithStdin(cmd string, args []string, executionContext context.Context, stdin io.Reader) ([]byte, error) {
	//register the context so that we have a chance to cancel the commands later
	ctx := c.registerContext(executionContext)
	if ctx == nil {
		return nil, context.Canceled
	}
	defer c.openExecutions.Done()
	defer c.releaseContext(ctx)

	command := exec.CommandContext(ctx, cmd, args...)
	configureProcess(command)
	//safety net: if cancellation cannot close the I/O pipes (e.g. a detached
	//grandchild escaped the process group), do not wait forever
	command.WaitDelay = 2 * time.Second
	if stdin != nil {
		command.Stdin = stdin
	}
	out, execErr := command.CombinedOutput()

	if len(out) > 0 {
		//trim combined output
		out = []byte(strings.Trim(string(out), " \n"))
	}

	if execErr != nil && ctx.Err() == nil {
		slog.Error("Command execution failed.", "error", execErr)
	} else if ctx.Err() != nil {
		slog.Info("Command execution cancelled.")
		return out, ctx.Err()
	}

	return out, execErr
}

func (c *CommandExecutor) registerContext(parentContext context.Context) context.Context {
	//lock to ensure the map is thread-safe
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.closed {
		return nil
	}
	c.openExecutions.Add(1)

	//wrap the given context so that we can later cancel the context (see Close func)
	ctx, cancelFunc := context.WithCancel(parentContext)
	c.usedContext[ctx] = cancelFunc

	return ctx
}

func (c *CommandExecutor) releaseContext(ctx context.Context) {
	//lock to ensure the map is thread-safe
	c.lock.Lock()
	defer c.lock.Unlock()

	//call the cancel func for the given context to release the resources
	c.usedContext[ctx]()

	delete(c.usedContext, ctx)
}

func (c *CommandExecutor) Close(timeout time.Duration) error {
	c.lock.Lock()
	c.closed = true
	for _, cancel := range c.usedContext {
		cancel()
	}
	c.lock.Unlock()

	done := make(chan struct{})
	go func() {
		c.openExecutions.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout exceeded")
	}
}
