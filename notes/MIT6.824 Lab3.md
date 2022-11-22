# MIT6.824 Lab3

在本实验中，需要实现一个分布式的KV存储库。在这里，分布式为KV存储库提供了容错的能力，即只要分布式集群中大多数KV存储库服务器可以工作，集群就可以为客户端提供KV存储库服务。其中，分布式KV存储库使用Raft库，使得客户端的操作将以日志的形式在服务器间进行复制，以保证系统的强一致性。本实验中，客户端所连续发送的操作需要在平均三分之一Raft心跳间隔内被提交，这使得需要对Lab2中的Raft实现进行更改。

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

客户端通过`clerk`向KV存储库服务器发送RPC请求，并由服务器完成所对应的操作，并通过RPC返回给客户端操作结果。在这里，`leader`用于标识当前的领导者服务器索引，避免每次发送RPC时重新寻找领导者；`clentId`表示客户端ID，`sequenceNum`表示该客户端下一个操作的操作序号，`clentId`与`sequenceNum`共同唯一标识各客户端所发出的操作。

```go
func (ck *Clerk) Get(key string) string {
	atomic.AddInt64(&ck.sequenceNum, 1)
	DPrintf("[client] Client try Get Key = %s", key)
	if ck.leader != -1 {
		args := GetArgs{Key: key, ClientId: ck.clentId, SequenceNum: ck.sequenceNum}
		reply := GetReply{}
		ok := ck.servers[ck.leader].Call("KVServer.Get", &args, &reply)
		if ok && reply.Err == "" {
			DPrintf("[client over] Get Key = %s OVER, val = %s", key, reply.Value)
			return reply.Value
		}
	}
	for {
		for i := range ck.servers {
			args := GetArgs{Key: key, ClientId: ck.clentId, SequenceNum: ck.sequenceNum}
			reply := GetReply{}
			ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
			if ok && reply.Err == "" {
				DPrintf("[client over] Get Key = %s OVER, val = %s", key, reply.Value)
				ck.leader = i
				return reply.Value
			}

		}
	}
}
```

```go
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.
	DPrintf("[client] Client try PutAppend key = %s,val = %s,op = %s", key, value, op)
	atomic.AddInt64(&ck.sequenceNum, 1)
	if ck.leader != -1 {
		args := PutAppendArgs{Key: key, Value: value, Op: op, ClientId: ck.clentId, SequenceNum: ck.sequenceNum}
		reply := PutAppendReply{}
		ok := ck.servers[ck.leader].Call("KVServer.PutAppend", &args, &reply)
		if ok && reply.Err == "" {
			DPrintf("[client over] Client PutAppend key = %s,val = %s,op = %s over", key, value, op)
			return
		}
	}
	for {
		for i := range ck.servers {
			args := PutAppendArgs{Key: key, Value: value, Op: op, ClientId: ck.clentId, SequenceNum: ck.sequenceNum}
			reply := PutAppendReply{}
			ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
			if ok && reply.Err == "" {
				DPrintf("[client over] Client PutAppend key = %s,val = %s,op = %s over", key, value, op)
				ck.leader = i
				return
			}
		}
	}
}
```

在`Get`及`PutAppend`中，客户端向服务器发送RPC请求以完成对应的操作。在每次操作进行前，需要将`ck.sequenceNum`原子性的增加，以唯一标识客户端所发出的操作。如`ck.leader`不为空，则尝试向该服务器发送RPC。当RPC请求成功且返回`reply.Err`为空时，代表本次操作成功。如操作不成功，则轮询所有服务器，直到操作成功，并将`ck.leader`置为使得操作成功的服务器。

```go
type PutAppendArgs struct {
	Key   string
	Value string
	Op    string // "Put" or "Append"
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	ClientId    int64
	SequenceNum int64
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Key string
	// You'll have to add definitions here.
	ClientId    int64
	SequenceNum int64
}

type GetReply struct {
	Err   Err
	Value string
}
```

RPC的请求及返回参数除了操作函数本身的参数及返回值外，还带有标识是否操作成功的`Err`及标识操作的`ClientId`及`SequenceNum`。

## 服务端实现

```go
type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	data       map[string]string
	clientSeq  map[int64]int64
	applyIndex int
}
```

在服务器`KVServer`中，使用`data`记录键值记录、`clientSeq`记录从各个客户端中所接受到操作的最大序号，用于去除重复的操作、`applyIndex`记录服务器所应用命令的最高序号，用于压缩日志。这三者作为服务器的运行参数，需要在其运行过程中被持久化保存。

```go
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.
	kv.data = map[string]string{}
	kv.clientSeq = map[int64]int64{}
	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	// You may need initialization code here.
	var data map[string]string
	var clientSeq map[int64]int64
	var applyIndex int
	snapshot := persister.ReadSnapshot()
	reader := bytes.NewBuffer(snapshot)
	decoder := labgob.NewDecoder(reader)
	if decoder.Decode(&data) == nil &&
		decoder.Decode(&clientSeq) == nil &&
		decoder.Decode(&applyIndex) == nil {
		kv.mu.Lock()
		kv.data = data
		kv.clientSeq = clientSeq
		kv.applyIndex = applyIndex
		kv.mu.Unlock()
	}
	go kv.receiveMsg()
	go kv.trysnapshot()
	return kv
}

```

在本实验中，服务器调用`kv.receiveMsg()`用于接收来自Raft端被提交的日志，并调用`kv.trysnapshot()`定期查看Raft日志的当前长度，并在其大于一定值后压缩日志。在服务器被启动时，通过`persister`参数还原其备份的运行参数。

```go
func (kv *KVServer) receiveMsg() {
	for msg := range kv.applyCh {
		if kv.killed() {
			return
		}
		DPrintf("[server %d],msg receive %v,raftstatesize = %d", kv.me, msg, kv.rf.RaftStateSize())
		if msg.CommandValid {
			op := msg.Command.(Op)
			kv.mu.Lock()
			kv.DoOperation(op)
			kv.applyIndex = msg.CommandIndex
			kv.mu.Unlock()
		} else if msg.SnapshotValid {
			var data map[string]string
			var clientSeq map[int64]int64
			reader := bytes.NewBuffer(msg.Snapshot)
			decoder := labgob.NewDecoder(reader)
			if decoder.Decode(&data) == nil &&
				decoder.Decode(&clientSeq) == nil {
				kv.mu.Lock()
				kv.data = data
				kv.clientSeq = clientSeq
				kv.applyIndex = msg.SnapshotIndex
				kv.mu.Unlock()
			}
		}
	}

}
```

在`receiveMsg()`中，服务器接受来自`kv.applyCh`的日志，直到其被杀死。每当其接收到日志时，其先判断该日志的类型，如其为命令日志，则调用`kv.DoOperation(op)`完成该命令对应的操作，并记录命令的`CommandIndex`至`kv.applyIndex`。如其为快照日志，则将快照中的服务器运行参数替换至本地。

```go
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Type        int // 0 - Get, 1 - Put, 2 - Append
	Key         string
	Val         string
	ClientId    int64
	SequenceNum int64
}

func (kv *KVServer) DoOperation(op Op) {
	if kv.clientSeq[op.ClientId] >= op.SequenceNum {
		return
	}
	kv.clientSeq[op.ClientId] = op.SequenceNum
	if op.Type == PUT {
		kv.data[op.Key] = op.Val
	} else if op.Type == APPEND {
		ret := kv.data[op.Key]
		kv.data[op.Key] = ret + op.Val
	}
}
```

在这里，`Op`类型用于标识操作，其中`Type`标识了操作的类型，`Key`和`Val`标识了操作的参数，`ClientId`和`SequenceNum`用于唯一标识客户端所发出的操作。在`DoOperation`中，对所需进行的操作的`SequenceNum`与`kv.clientSeq[op.ClientId]`比较，如前者大于等于后者，由于客户端所发出的操作序号为单调递增的，则该操作已经被执行，不应被再次执行；如前者小于后者，则该操作尚未被执行，则执行相应的操作并更新`kv.clientSeq[op.ClientId]`。

```go
func (kv *KVServer) trysnapshot() {
	for !kv.killed() {
		if kv.maxraftstate > 0 && kv.rf.RaftStateSize() > kv.maxraftstate*8/10 {
			kv.mu.Lock()
			writer := new(bytes.Buffer)
			encoder := labgob.NewEncoder(writer)
			encoder.Encode(kv.data)
			encoder.Encode(kv.clientSeq)
			encoder.Encode(kv.applyIndex)
			applyindex := kv.applyIndex
			snapshot := writer.Bytes()
			kv.rf.Snapshot(applyindex, snapshot)
			kv.mu.Unlock()
		}
		time.Sleep(5 * time.Millisecond)
	}
}
```

在`trysnapshot()`中，服务器轮询当前Raft日志的长度，如其长度高于阈值的0.8倍，则调用`kv.rf.Snapshot(applyindex, snapshot)`压缩日志，`applyindex`标识了当前已被运行的最高操作序号、`snapshot`保存了服务器的运行参数。

```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	if kv.killed() {
		reply.Err = "[server] Server is killed"
		return
	}
	_, _, isleader := kv.rf.Start(Op{Type: GET, Key: args.Key, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
	if !isleader {
		reply.Err = "[server] Server is not leader"
		return
	}
	DPrintf("[server %d] Get : clientId : %d,seq : %d,key : %s", kv.me, args.ClientId, args.SequenceNum, args.Key)
	var timeout int32 = 0
	go func() {
		time.Sleep(1000 * time.Millisecond)
		atomic.StoreInt32(&timeout, 1)
	}()
	for {
		if atomic.LoadInt32(&timeout) != 0 {
			reply.Err = "Timeout"
			return
		}
		kv.mu.Lock()
		if kv.clientSeq[args.ClientId] >= args.SequenceNum {
			reply.Value = kv.data[args.Key]
			kv.mu.Unlock()
			return
		}
		kv.mu.Unlock()
	}
}
func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	if kv.killed() {
		reply.Err = "Server is killed"
		return
	}
	var opType int
	if args.Op == "Put" {
		opType = PUT
	} else if args.Op == "Append" {
		opType = APPEND
	}
	_, _, isleader := kv.rf.Start(Op{Type: opType, Key: args.Key, Val: args.Value, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
	if !isleader {
		reply.Err = "Server is not leader"
		return
	}
	DPrintf("[server %d] PutAppend : clientId : %d,seq : %d,key : %s, val : %s, Op : %s", kv.me, args.ClientId, args.SequenceNum, args.Key, args.Value, args.Op)
	var timeout int32 = 0
	go func() {
		time.Sleep(1000 * time.Millisecond)
		atomic.StoreInt32(&timeout, 1)
	}()
	for {
		if atomic.LoadInt32(&timeout) != 0 {
			reply.Err = "Timeout"
			return
		}
		kv.mu.Lock()
		if kv.clientSeq[args.ClientId] >= args.SequenceNum {
			kv.mu.Unlock()
			return
		}
		kv.mu.Unlock()
	}
}
```

在`Get`及`PutAppend`RPC处理函数中，需要判断当前服务器是否存活，如已被杀死则填充`reply.Err`并返回。然后，调用`kv.rf.Start`将客户端发送的操作以命令日志的形式传入Raft端，如返回结果表示本地Raft不是领导者，则填充`reply.Err`并返回。在命令被传入Raft后，需要轮询等待该命令被提交。在这里，通过当前操作的序号是否小于` kv.clientSeq[args.ClientId]`即可知道该命令是否被提交并执行。为了防止命令在提交后丢失，从而导致死循环，在这里设置了1秒的超时阈值，在等待1秒后填充`reply.Err`并返回。

## 对Raft的更改

本实验中，客户端所连续发送的操作需要在平均三分之一Raft心跳间隔内被提交，这使得需要对Lab2中的Raft实现进行更改。为了快速的提交日志，Raft需要在调用`Start`时立即向跟随者发送日志，并在发送后即时地更新`commitIndex`。

```go
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	_, isLeader := rf.GetState()
	if !isLeader || rf.killed() {
		DPrintf("[%d] Start() Fail isleader = %t, isKilled = %t", rf.me, isLeader, rf.killed())
		return -1, -1, false
	}
	// Your code here (2B).
	//atomic.StoreInt32(&rf.applying, 1)
	rf.mu.Lock()
	logEntry := LogEntry{Command: command, Term: rf.CurrentTerm, Index: rf.GetLastEntry().Index + 1}
	rf.Log = append(rf.Log, logEntry)
	rf.persist()
	rf.mu.Unlock()
	DPrintf("[%d] Start() ,index : %d,term : %d,command : %d", rf.me, logEntry.Index, logEntry.Term, command)
	atomic.StoreInt32(&rf.quicklyCheck, 20)
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go rf.SendEntries(i)
		}

	}
	return logEntry.Index, logEntry.Term, isLeader
}
```

在这里，改进后的Raft将在调用`Start`后立即向跟随着发送日志，同时将`rf.quicklyCheck`原子地置为20，使得Raft在一定时间内不进行心跳间隔长度地睡眠，从而快速地更新`commitIndex`。

```go
func (rf *Raft) leaderTask() {
	rf.mu.Unlock()
	if atomic.LoadInt32(&rf.quicklyCheck) <= 0 {
		rf.TrySendEntries(false)
		rf.UpdateCommitIndex()
		time.Sleep(time.Duration(rf.BroadcastTime) * time.Millisecond)
	} else {
		rf.UpdateCommitIndex()
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&rf.quicklyCheck, -1)
	}

}
func (rf *Raft) ticker() {
	//peersNum := len(rf.peers)
	for rf.killed() == false {
		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		rf.mu.Lock()
		rf.UpdateApplied()
		State := rf.State
		if State == LEADER {
			rf.leaderTask()
		} else if State == FOLLOWER {
			DPrintf("[%d] ElectionTimeout = %d", rf.me, rf.ElectionTimeout)
			if rf.ElectionTimeout < rf.BroadcastTime {
				rf.State = CANDIDATE
				DPrintf("[%d] ElectionTimeout,convert to CANDIDATE\n", rf.me)
				rf.mu.Unlock()
				continue
			}
			rf.ElectionTimeout -= rf.BroadcastTime
			rf.mu.Unlock()
			time.Sleep(time.Duration(rf.BroadcastTime) * time.Millisecond)
		} else if State == CANDIDATE {
			rf.doElection()
		}
	}
}
```

在`ticker中`，使用`rf.leaderTask()`代替了原有的领导者所需执行的任务。在`rf.leaderTask()`中，当`rf.quicklyCheck`大于零时，则不调用`rf.TrySendEntries(false)`发送心跳信息、仅睡眠1毫秒并递减`rf.quicklyCheck`；当其等于零时，则正常地调用`rf.TrySendEntries(false)`并睡眠心跳间隔时长。

可以看出，每当Raft调用`Start`，其将在20毫秒内快速地尝试更新`commitIndex`。当Raft集群未发生故障且网络状态良好时，日志在`Start`中被发送后将被跟随者快速的接受，该机制使得已经被大多数服务器接受的日志可以被及时的提交，以降低客户端的操作命令延迟。

## 实验结果

本实验在100次测试中不出错，证明其正确性及稳定性。

```
Test: one client (3A) ...
  ... Passed --  15.1  5  6599  852
Test: ops complete fast enough (3A) ...
  ... Passed --  15.4  3  5278    0
Test: many clients (3A) ...
  ... Passed --  15.5  5 10286 1002
Test: unreliable net, many clients (3A) ...
  ... Passed --  16.7  5  5896  868
Test: concurrent append to same key, unreliable (3A) ...
  ... Passed --   1.6  3   287   52
Test: progress in majority (3A) ...
  ... Passed --   1.4  5   149    2
Test: no progress in minority (3A) ...
  ... Passed --   1.0  5   109    3
Test: completion after heal (3A) ...
  ... Passed --   1.0  5    56    3
Test: partitions, one client (3A) ...
  ... Passed --  23.0  5 15046  738
Test: partitions, many clients (3A) ...
  ... Passed --  22.8  5 26032  860
Test: restarts, one client (3A) ...
  ... Passed --  22.0  5 16321  940
Test: restarts, many clients (3A) ...
  ... Passed --  24.1  5 27510 1101
Test: unreliable net, restarts, many clients (3A) ...
  ... Passed --  25.2  5  7999  913
Test: restarts, partitions, many clients (3A) ...
  ... Passed --  30.0  5 24874  923
Test: unreliable net, restarts, partitions, many clients (3A) ...
  ... Passed --  30.7  5  7241  610
Test: unreliable net, restarts, partitions, random keys, many clients (3A) ...
  ... Passed --  35.1  7 20109  621
Test: InstallSnapshot RPC (3B) ...
  ... Passed --   6.0  3  2727   63
Test: snapshot size is reasonable (3B) ...
  ... Passed --   4.5  3  5520  800
Test: ops complete fast enough (3B) ...
  ... Passed --   5.6  3  6877    0
Test: restarts, snapshots, one client (3B) ...
  ... Passed --  21.4  5 24791 2981
Test: restarts, snapshots, many clients (3B) ...
  ... Passed --  23.1  5 35850 3447
Test: unreliable net, snapshots, many clients (3B) ...
  ... Passed --  18.1  5  9191 1456
Test: unreliable net, restarts, snapshots, many clients (3B) ...
  ... Passed --  26.6  5 10685 1349
Test: unreliable net, restarts, partitions, snapshots, many clients (3B) ...
  ... Passed --  31.3  5  6668  883
Test: unreliable net, restarts, partitions, snapshots, random keys, many clients (3B) ...
  ... Passed --  33.5  7 18441 1265
PASS
```

