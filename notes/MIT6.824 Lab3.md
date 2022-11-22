# MIT6.824 Lab3

在本实验中，需要实现一个分布式的KV存储库。在这里，分布式为KV存储库提供了容错的能力，即只要分布式集群中大多数KV存储库服务器可以工作，集群就可以为客户端提供KV存储库服务。其中，分布式KV存储库使用Raft库，使得客户端的操作将以日志的形式在服务器间进行复制，以保证系统的强一致性。

## 客户端实现

本实验所实现的KV存储库需要提供给客户端三个接口：`Get(key string) string`、`Put(key string, value string)`及`Append(key string, value string)`。其中，`Get`获取对应键`key`所对应的值；`Put`将对应键`key`映射到值`value`；`Append`将对应键`key`所映射的值加上`value`，如对应键无映射的值，则本函数的功能等同于`Put`。

```go
type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.
	leader      int
	clentId     int64
	sequenceNum int64
}
func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	ck.leader = -1
	ck.sequenceNum = 0
	ck.clentId = nrand()
	return ck
}
```

客户端通过`clerk`向KV存储库服务器发送RPC请求，并由服务器完成所对应的操作，并通过RPC返回给客户端操作结果。在这里，`leader`用于标识当前的领导者服务器索引，避免每次发送RPC时重新寻找领导者；`clentId`表示客户端ID，`sequenceNum`表示该客户端下一个操作的操作序号，`clentId`与`sequenceNum`共同独一标识各客户端所发出的操作。



