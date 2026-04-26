## Issues

- **ShadowConsumer consumeShadow blocking Next() issue**: The `consumeShadow` method had a `select` statement checking `sc.cancelCh` and `ctx.Done()` BEFORE calling `loserStream.Next()`. Once `Next()` was called and blocked (e.g., on `neverEndingStream`), the select was bypassed and cancel signals couldn't interrupt. Fixed by wrapping `Next()` in a goroutine and selecting on the result channel along with cancel/done channels.

**Solution**: The goroutine + channel pattern ensures `Next()` is always interruptible:
```go
nextCh := make(chan struct{})
var ok bool
go func() {
    ok = loserStream.Next()
    close(nextCh)
}()
select {
case <-sc.cancelCh:
    // handle cancel
case <-ctx.Done():
    // handle context done
case <-nextCh:
    // Next() returned, continue
}
```

**Tests verified**: `TestShadowConsumer_ServerShutdown` passes reliably (was timing out after 120s before)
