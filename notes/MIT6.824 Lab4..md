# MIT6.824 Lab4

在本实验中，需要实现分片KV存储系统，其中系统的键将被映射至对应的分片中，各分片将被均匀的分配到若干副本组中进行服务，即每个副本组仅需处理其对应的分片的操作，这使得不同组之间可并行操作，以提升系统的性能。

在这里，分片KV存储系统可以被分为两个部分，其一为**“分片控制器”**，其决定哪个副本组应当为哪些分片服务，以及副本组所对应的服务器集群，该信息也被称作为**配置**；其二为**“副本组”**，是由一组KV存储服务器所组成的Raft集群，其通过访问分片控制器来获取当前的配置信息，并为对应的分片提供服务，当配置更改时，副本组应当将其拥有的分片进行转移。

## 分片控制器

```go
type Config struct {
	Num    int              // config number
	Shards [NShards]int     // shard -> gid
	Groups map[int][]string // gid -> servers[]
}
```

分片控制器管理多个编号的配置（Config），配置中描述了分片到副本组的分配（Shards）及副本组中的服务器明细（Groups）。每当配置更改时，分片控制器都会在其中存储新的配置，并可由KV存储服务器访问任一编号的配置信息。

具体而言，分片控制器应当提供如下的RPC接口：

- `Join`：该接口被用于向当前配置（编号最高的置）中添加新的副本组，在新的配置中，分片应当被均匀的分配至新配置中的副本组；
- `Leave`：该接口被用于从当前配置中删除若干副本组，并使得分片在新的配置中被重新均匀分配；
- `Move`：该接口使得对应的分片被分配至对应的副本组，在新的配置中分片无需被重新分配；
- `Query`：该接口使得用户可以访问对应编号的配置信息，当其访问的编号为-1或大于当前已知的最大配置编号时，分片控制器应当使用最新的配置回复。

```go
type ShardCtrler struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	// Your data here.
	configs     []Config // indexed by config num
	clientSeq   map[int64]int64
	queryBuffer map[int64]Config
}
```

在分片控制器的数据结构中，`configs`中存储分片控制器所管理的配置、`clientSeq`用于过滤掉重复的客户端请求、`queryBuffer`中存储了各客户端执行`Query`时所应返回的配置。

```go
type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	clentId     int64
	sequenceNum int64
}
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// Your code here.
	ck.sequenceNum = 0
	ck.clentId = nrand()
	return ck
}
```

其中，客户端通过`Clerk`访问分片控制器提供的接口，`Clerk`被初始化时被分配一个随机的`clentId`用于标识自身，并在每次调用其任意接口时原子的增加`sequenceNum`用于标识该客户端的每个操作。通过`clentId`与`sequenceId`，任意客户端的任意操作均可被唯一标识，用于KV存储服务器的去重操作，具体的方法与Lab3中相同。

```go
type Op struct {
	// Your data here.
	Type        int // 0 - Join, 1 - Leave, 2 - Move, 3 -Query, 3 - AskShards
	Servers     map[int][]string
	GIDs        []int
	Shard       int
	GID         int
	Num         int
	ClientId    int64
	SequenceNum int64
}
```

在命令结构体`Op`中，其拥有接口参数的所有字段，当分片控制器执行命令时，其对应的填充相应的字段。

```go
func (sc *ShardCtrler) Move(args *MoveArgs, reply *MoveReply) {
	// Your code here.
	_, _, isleader := sc.rf.Start(Op{Type: MOVE, Shard: args.Shard, GID: args.GID, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
	if !isleader {
		return
	}
	var timeout int32 = 0
	go func() {
		time.Sleep(1000 * time.Millisecond)
		atomic.StoreInt32(&timeout, 1)
	}()
	for {
		if atomic.LoadInt32(&timeout) != 0 {
			return
		}
		sc.mu.Lock()
		if sc.clientSeq[args.ClientId] >= args.SequenceNum {
			sc.mu.Unlock()
			reply.Done = true
			return
		}
		sc.mu.Unlock()
	}
}
```

对于`Join`、`Leave`及`Move`三个写函数，其调用`Start`向Raft传入当前操作的日志，若当前服务器并非领导者，则直接返回，否则则在1000ms内等待当前操作完成，在这里，通过比较当前操作的序号`args.SequenceNum`与服务器完成操作的编号` sc.clientSeq[args.ClientId]`来判断当前操作是否完成，如在超时时间内完成，则设置`reply.Done`用于标识操作完成，否则则直接返回。

```go
func (sc *ShardCtrler) Query(args *QueryArgs, reply *QueryReply) {
	// Your code here.
	_, _, isleader := sc.rf.Start(Op{Type: QUERY, Num: args.Num, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
	if !isleader {
		return
	}
	var timeout int32 = 0
	go func() {
		time.Sleep(1000 * time.Millisecond)
		atomic.StoreInt32(&timeout, 1)
	}()
	for {
		if atomic.LoadInt32(&timeout) != 0 {
			return
		}
		sc.mu.Lock()
		if sc.clientSeq[args.ClientId] >= args.SequenceNum {
			reply.Config = sc.queryBuffer[args.ClientId]
			sc.mu.Unlock()
			reply.Done = true
			return
		}
		sc.mu.Unlock()
	}
}
```

对于`Query`读函数，其在当前操作完成后通过访问`sc.queryBuffer[args.ClientId]`来获取读取结果并返回，`sc.queryBuffer[args.ClientId]`在对应客户端所发送的`Query`操作被提交后所填充，由于单个客户端串行访问分片控制器，因此以`clientId`为粒度保存读取结果是安全的。

```go
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardCtrler {
	sc := new(ShardCtrler)
	sc.me = me

	sc.configs = make([]Config, 1)
	sc.configs[0].Groups = map[int][]string{}
	for i := 0; i < NShards; i++ {
		sc.configs[0].Shards[i] = 0
	}
	labgob.Register(Op{})
	sc.applyCh = make(chan raft.ApplyMsg)
	sc.rf = raft.Make(servers, me, persister, sc.applyCh, "SM")

	// Your code here.
	sc.queryBuffer = make(map[int64]Config)
	sc.clientSeq = make(map[int64]int64)
	go sc.receiveMsg()
	return sc
}
func (sc *ShardCtrler) receiveMsg() {
	for msg := range sc.applyCh {
		op := msg.Command.(Op)
		sc.mu.Lock()
		DPrintf("[server %d],msg receive %v,", sc.me, msg)
		sc.DoOperation(op)
		sc.mu.Unlock()
	}
}
```

在分片控制器初始化时，其开启`sc.receiveMsg()`线程，用于接受Raft中提交的命令，接受到的命令被传入`sc.DoOperation(op)`执行。

```go
func (sc *ShardCtrler) DoOperation(op Op) {
	if sc.clientSeq[op.ClientId] >= op.SequenceNum {
		return
	}
	sc.clientSeq[op.ClientId] = op.SequenceNum
	configSize := len(sc.configs)
	if op.Type == JOIN {
		sc.doJoin(op)
	} else if op.Type == LEAVE {
		sc.doLeave(op)
	} else if op.Type == MOVE {
		sc.doMove(op)
	} else if op.Type == QUERY {
		if op.Num >= configSize || op.Num == -1 {
			sc.queryBuffer[op.ClientId] = sc.configs[configSize-1]
		} else {
			sc.queryBuffer[op.ClientId] = sc.configs[op.Num]
		}
	}
}
```

在`DoOperation`中，使用`clientId`与`SequenceNum`对操作去重，并更新`sc.clientSeq[op.ClientId]`以表明对应操作的完成。对于`JOIN`、`LEAVE`、`MOVE`操作，其调用分别的函数完成操作；对于`Query`操作，其在`sc.queryBuffer[op.ClientId]`中缓存此时对应编号的配置结果，以防止配置结果在返回客户端前被其他操作修改。

```go
func (sc *ShardCtrler) doJoin(op Op) {
	configSize := len(sc.configs)
	config := Config{Num: configSize}
	groups := make(map[int][]string)
	for k, v := range sc.configs[configSize-1].Groups {
		groups[k] = v
	}
	for k, v := range op.Servers {
		groups[k] = v
	}
	config.Groups = groups
	var gids []int
	for k := range groups {
		gids = append(gids, k)
	}
	shards := Balance(NShards, gids)
	for i := 0; i < NShards; i++ {
		config.Shards[i] = shards[i]
	}
	sc.configs = append(sc.configs, config)
	DPrintf("[server %d],msg JOIN gids = %v,shards = %v", sc.me, gids, config.Shards)
}
```

在`doJoin`中，其将当前最新配置与操作参数中的副本组信息结合，并得到新的副本组信息，通过`Balance`函数，分片被均匀的分配到各个副本组，并得到分配结果，最后，将新配置存储至分片控制器。

```go
func Balance(shards int, gids []int) []int {
	sz := len(gids)
	sort.Ints(gids)
	ret := make([]int, shards)
	idx := 0
	for i := 0; i < sz; i++ {
		n := 0
		if i < shards%sz {
			n = shards/sz + 1
		} else {
			n = shards / sz
		}
		for j := 0; j < n; j++ {
			ret[idx+j] = gids[i]
		}
		idx += n
	}
	return ret
}
```

在`Balance`中，其输入组ID编号的数组`gids`及分片数量`shards`，填充长度为`shards`的分配结果数组并返回，其中不同组ID将均匀的分布在分配结果数组中，保证拥有分片最多与最少的组的分片数量差不超过一。

```go
func (sc *ShardCtrler) doLeave(op Op) {
	configSize := len(sc.configs)
	config := Config{Num: configSize}
	groups := make(map[int][]string)
	for k, v := range sc.configs[configSize-1].Groups {
		groups[k] = v
	}
	for _, k := range op.GIDs {
		delete(groups, k)
	}
	config.Groups = groups
	var gids []int
	for k := range groups {
		gids = append(gids, k)
	}
	shards := Balance(NShards, gids)
	for i := 0; i < NShards; i++ {
		config.Shards[i] = shards[i]
	}
	sc.configs = append(sc.configs, config)
	DPrintf("[server %d],msg Leave gids = %v,shards = %v", sc.me, gids, config.Shards)
}
```

在`doLeave`中，其从当前最新配置中删除操作参数中的副本组，并得到新的副本组信息，通过`Balance`函数，分片被均匀的分配到各个副本组，并得到分配结果，最后，将新配置存储至分片控制器。

```go
func (sc *ShardCtrler) doMove(op Op) {
	configSize := len(sc.configs)
	config := Config{Num: configSize}
	groups := make(map[int][]string)
	for k, v := range sc.configs[configSize-1].Groups {
		groups[k] = v
	}
	config.Groups = groups
	for i := 0; i < NShards; i++ {
		config.Shards[i] = sc.configs[configSize-1].Shards[i]
	}
	config.Shards[op.Shard] = op.GID
	sc.configs = append(sc.configs, config)
	DPrintf("[server %d],msg MOVE Shard = %d,GID = %d,shards = %v", sc.me, op.Shard, op.GID, config.Shards)
}
```

在`doMove`中，其将当前最新配置中某分片分配至指定副本组，并将新配置存储至分片控制器。

## 分片KV存储系统

### 客户端

```go
type Clerk struct {
	sm       *shardctrler.Clerk
	config   shardctrler.Config
	make_end func(string) *labrpc.ClientEnd
	// You will have to modify this struct.
	clentId     int64
	sequenceNum []int64
}
func MakeClerk(ctrlers []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.sm = shardctrler.MakeClerk(ctrlers)
	ck.make_end = make_end
	// You'll have to add code here.
	Nshards := len(ck.config.Shards)
	ck.sequenceNum = make([]int64, Nshards)
	ck.clentId = nrand()
	return ck
}

func (ck *Clerk) Get(key string) string {
	args := GetArgs{}
	args.Key = key
	shard := key2shard(key)
	atomic.AddInt64(&ck.sequenceNum[shard], 1)
	args.ClientId = ck.clentId
	args.SequenceNum = ck.sequenceNum[shard]
	for {
		gid := ck.config.Shards[shard]
		args.Shard = shard
		args.Gid = gid
		DPrintf("[client] Client try Get shard = %d,gid = %d", shard, gid)
		if servers, ok := ck.config.Groups[gid]; ok {
			// try each server for the shard.
			for si := 0; si < len(servers); si++ {
				srv := ck.make_end(servers[si])
				var reply GetReply
				ok := srv.Call("ShardKV.Get", &args, &reply)
				if ok && (reply.Err == OK || reply.Err == ErrNoKey) {
					return reply.Value
				}
				if ok && (reply.Err == ErrWrongGroup) {
					break
				}      
			}
		}
		time.Sleep(100 * time.Millisecond)
		// ask controler for the latest configuration.
		ck.config = ck.sm.Query(-1)
	}

	return ""
}
```

客户端的操作与Lab3中大致相同，不同之处在于客户端需要对不同分片维护不同的`sequenceNum`，使得不同分片的操作可以被独立的唯一标识。

### 服务端

```go
type ShardKV struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	make_end     func(string) *labrpc.ClientEnd
	gid          int
	maxraftstate int // snapshot if log grows this big
	NShards      int
	sm           *shardctrler.Clerk
	// Your definitions here.
	config     shardctrler.Config
	data       []map[string]string
	clientSeq  []map[int64]int64
	applyIndex int
	initialize bool

	currShards []bool
	needShards []bool
	tasks      []moveShardTask
	serverSeq  []map[int64]int64
}
```

在分片KV存储系统中，其为不同的分片（Shard）分配不同的KV存储服务数据结构，即键值映射（`data`）、客户端完成操作记录（`clientSeq`）。

其中，`currShards`记录了KV存储服务器中拥有的分片情况、`needShards`记录了KV存储服务器在当前配置下所需要的分片、`tasks`记录了当前的分片转移任务、`serversSeq`为记录了分片转移操作的完成记录、`initialize`用于记录该服务器是否观察到了配置的初始化，用于在最开始将分片分配至某一副本组。

```go
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int, gid int, ctrlers []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *ShardKV {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(ShardKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.make_end = make_end
	kv.gid = gid
	kv.sm = shardctrler.MakeClerk(ctrlers)
	kv.NShards = len(kv.config.Shards)

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh, "KV-"+strconv.Itoa(gid))
	kv.data = make([]map[string]string, kv.NShards)
	kv.clientSeq = make([]map[int64]int64, kv.NShards)
	kv.readBuffer = make([]map[int64]string, kv.NShards)
	kv.currShards = make([]bool, kv.NShards)
	kv.needShards = make([]bool, kv.NShards)
	kv.serverSeq = make([]map[int64]int64, kv.NShards)
	for i := 0; i < kv.NShards; i++ {
		kv.data[i] = make(map[string]string)
		kv.clientSeq[i] = make(map[int64]int64)
		kv.serverSeq[i] = make(map[int64]int64)
		kv.readBuffer[i] = make(map[int64]string)
	}
	var data []map[string]string
	var clientSeq []map[int64]int64
	var readBuffer []map[int64]string
	var initialize bool
	var currShards []bool
	var tasks []moveShardTask
	var serverSeq []map[int64]int64
	snapshot := persister.ReadSnapshot()
	reader := bytes.NewBuffer(snapshot)
	decoder := labgob.NewDecoder(reader)
	if decoder.Decode(&data) == nil &&
		decoder.Decode(&clientSeq) == nil &&
		decoder.Decode(&readBuffer) == nil &&
		decoder.Decode(&initialize) == nil &&
		decoder.Decode(&currShards) == nil &&
		decoder.Decode(&tasks) == nil &&
		decoder.Decode(&serverSeq) == nil {
		kv.mu.Lock()
		kv.data = data
		kv.clientSeq = clientSeq
		kv.readBuffer = readBuffer
		kv.initialize = initialize
		kv.currShards = currShards
		kv.tasks = tasks
		kv.serverSeq = serverSeq
		kv.mu.Unlock()
	}
	go kv.updateConfig()
	go kv.receiveMsg()
	go kv.trysnapshot()
	go kv.MoveShard()
	DPrintf("[server %d.%d] Start", kv.me, kv.gid)
	return kv
}
```

当分片KV存储服务器被创建时，其读取在`persister`中保存的快照，并初始化数据结构，其中所有表明服务器运行状态的字段均需被快照保存。在初始化后，服务器开启`kv.updateConfig()`线程轮询并更新当前配置、`kv.receiveMsg()`用于接受Raft中提交的命令、`kv.trysnapshot()`检查当前Raft日志长度并压缩日志、`kv.MoveShard()`执行分片转移任务。

```go
func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	if kv.config.Shards[args.Shard] != kv.gid || !kv.currShards[args.Shard] {
        reply.Err = ErrWrongGroup
		kv.mu.Unlock()
		return
	}
	kv.mu.Unlock()
	var opType int
	if args.Op == "Put" {
		opType = PUT
	} else if args.Op == "Append" {
		opType = APPEND
	}
	_, _, isleader = kv.rf.Start(Op{Type: opType, Key: args.Key, Val: args.Value, ClientId: args.ClientId, SequenceNum: args.SequenceNum, Shard: args.Shard})
	if !isleader {
		reply.Err = ErrWrongLeader
		return
	}

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
		if kv.clientSeq[args.Shard][args.ClientId] >= args.SequenceNum {
			kv.mu.Unlock()
			reply.Err = OK
			return
		}
		kv.mu.Unlock()
	}
}
```

在`PutAppend`中，服务器需要检查当前配置中，该键所对应的分片是否在其副本组管理下，并检查其内部是否存有该分片，如不符合则返回`ErrWrongGroup`错误，其余操作与Lab3中相同，即发送操作命令至Raft，并在一定超时时间内等待命令被提交。

```go
func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.

	kv.mu.Lock()
	if kv.config.Shards[args.Shard] != kv.gid || !kv.currShards[args.Shard] {
		reply.Err = ErrWrongGroup
		kv.mu.Unlock()
		return
	}
	kv.mu.Unlock()
	_, _, isleader := kv.rf.Start(Op{Type: GET, Key: args.Key, ClientId: args.ClientId, SequenceNum: args.SequenceNum, Shard: args.Shard})
	if !isleader {
		reply.Err = ErrWrongLeader
		return
	}
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
		if kv.clientSeq[args.Shard][args.ClientId] >= args.SequenceNum {
			reply.Value = kv.data[args.Shard][args.Key]
			kv.mu.Unlock()
			reply.Err = OK
			return
		}
		kv.mu.Unlock()
	}

}
```

在`Get`中，服务器需要检查当前配置中，该键所对应的分片是否在其副本组管理下，并检查其内部是否存有该分片，如不符合则返回`ErrWrongGroup`错误，其余操作与Lab3中相同，即发送操作命令至Raft，并在一定超时时间内等待命令被提交。

```
Client1: |---WX1---|    |-----WX2---|
Client2:             |---------RX1--------|
```

注意到，上述情况仍满足线性一致性，因此对于`Get`无需使用分组控制器中的缓存方法缓存在`Get`命令被提交时的返回结果。

![lab4_figure1](..\noteFigures\lab4_figure1.jpg)

在本实验中，所设计的分片转移机制如上图所示，其流程如下：

1. 在`updateConfig()`中，服务器轮询检查内部是否存在`currShards`为真、`needShards`为假的分片，当检查到该种分片时，其向Raft发送`MOVETASK`命令，以开启分片转移并使得Raft集群获得共识。其中，`MOVETASK`中保存了分片的索引、其应当被发送的服务器、当前配置的编号；

2. 当`MOVETASK`命令被提交时，服务器检查其内部是否仍存在该分片（即`currShards`是否为真），若仍存在该分片，则创建`moveShardTask`分片转移任务。在任务中，其包含了该分片的`data`、`clientSeq`信息用于接受分片的服务器开展该分片的服务，以及该分片的索引、需要被发往的服务器、配置编号信息。在创建完任务后，服务器将该分片的数据删除，实现垃圾回收，并将任务存入`kv.tasks`，当服务器形成快照时，同样需要对`tasks`进行保存，以保证分片数据的持久性。

   同时，服务器需要对`MOVETASK`命令进行去重，其方法为通过分片索引及配置编号标识独一的`MOVETASK`，并在收到重复的<分片索引、配置编号>元组时抛弃命令。

3. 在`MoveShard()`中，服务器轮询当前服务器`tasks`中是否存在分片转移任务，如存在任务，则向任务中所注明的服务器集群发送`PutShard`RPC请求，并在RPC请求中包含分片转移任务的所有信息。

4. 当需求分片的服务器收到`PutShard`RPC请求时，其向Raft发送`PUTSHARD`命令，以部署分片并使得集群获得共识。当命令被提交时，对于发送端而言，服务器需要向发送端示意分片转移完成，发送端将命令从`tasks`中删除并完成分片转移操作。

   对于接收端而言，服务器需要对`PUTSHARD`命令进行降重操作，其方法为通过分片编号、发送端组ID及配置编号标识独一的`PUTSHARD`，并在收到重复的<分片索引、发送端组ID、配置编号>元组时抛弃命令。在去重操作后，服务器将分片的`data`及`clientSeq`部署至本地。

下文中，将对上述函数的实际代码进行讲解。

```go
func (kv *ShardKV) updateConfig() {
	for {
		kv.mu.Lock()
		config := kv.sm.Query(-1)
		_, isleader := kv.rf.GetState()
		kv.rf.Start(Op{})
		if config.Num != kv.config.Num && config.Num != 0 {
			if !kv.initialize {
				firstConfig := kv.sm.Query(1)
				for i := range kv.config.Shards {
					if firstConfig.Shards[i] == kv.gid {
						kv.currShards[i] = true
					}
				}
				kv.initialize = true
			}
			needShards := make([]bool, kv.NShards)
			for i := range kv.config.Shards {
				if config.Shards[i] == kv.gid {
					needShards[i] = true
				}
			}
			kv.needShards = needShards
			kv.config = config
		}
		for i := range kv.currShards {
			if !kv.needShards[i] && kv.currShards[i] {
				kv.rf.Start(Op{Type: MOVETASK, ConfigNum: kv.config.Num, Shard: i,
					ClientId: int64(kv.gid), SequenceNum: int64(kv.config.Num), Servers: kv.config.Groups[kv.config.Shards[i]]})
				}
			}
		}
		kv.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}
```

在`updateConfig()`中，服务器定期向分片控制器获取最新配置，当发现配置更新时，将最新配置部署至本地，并更新`needShards`。在这里，通过编号为1的配置完成分片的初始分配，其方法为当`initialize`为假时，服务器检查编号1配置中的组ID是否为本副本组的组ID，如是则将`currShards`置为全1，并将`initialize`置为真，在保存快照时，同样需要保存`initialize`信息，以防止分片被重复分配。

同时，服务器检查是否存在`needShards`为真、`currShards`为假的分片，如存在则针对该分片发送`MOVETASK`命令。

```go
func (kv *ShardKV) MoveShard() {
	for {
		kv.mu.Lock()
		if len(kv.tasks) != 0 {
			task := kv.tasks[0]
			kv.tasks = append(kv.tasks[:0], kv.tasks[1:]...)
			kv.DoMoveShardTask(task)
		}
		kv.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
}
func (kv *ShardKV) DoMoveShardTask(task moveShardTask) {
	DPrintf("[server %d.%d] DoMoveShardTask task is %v", kv.gid, kv.me, task)
	args := PutShardArgs{}
	args.ClientId = int64(kv.gid)
	args.SequenceNum = int64(task.ConfigNum)
	args.Shard = task.Shard
	args.Data = task.ShardData
	args.ClientSeq = task.ShardSeq
	args.Buffer = task.ShardBuffer
	servers := task.Servers
	kv.mu.Unlock()
	for {
		// try each server for the shard.
		for si := 0; si < len(servers); si++ {
			srv := kv.make_end(servers[si])
			var reply PutShardReply
			ok := srv.Call("ShardKV.PutShard", &args, &reply)
			if ok && reply.Done {
				kv.mu.Lock()
				return
			}
		}
	}
}
```

在` MoveShard()`中，服务器定期检查当前是否存在分片转移任务，如时则将其执行并删除任务。执行分片转移任务的方式为，将任务的参数通过`PutShard`发送至接收端服务器，直至RPC调用成功且发送方返回`reply.Done`为真。

```go
func (kv *ShardKV) PutShard(args *PutShardArgs, reply *PutShardReply) {
	_, _, isleader := kv.rf.Start(Op{Type: PUTSHARD, ShardData: args.Data, ShardSeq: args.ClientSeq, Shard: args.Shard,
		ClientId: args.ClientId, SequenceNum: args.SequenceNum})
	if !isleader {
		return
	}
	var timeout int32 = 0
	go func() {
		time.Sleep(1000 * time.Millisecond)
		atomic.StoreInt32(&timeout, 1)
	}()
	for {
		if atomic.LoadInt32(&timeout) != 0 {
			return
		}
		kv.mu.Lock()
		if kv.serverSeq[args.Shard][args.ClientId] >= args.SequenceNum {
			kv.mu.Unlock()
			reply.Done = true
			return
		}
		kv.mu.Unlock()
	}
}
```

当服务器收到`PutShard`时，其向Raft发送`PUTSHARD`任务，并在超时时间内等待命令被提交，如提交成功则向发送方发送`reply.Done`为真。

```go
func (kv *ShardKV) receiveMsg() {
	for msg := range kv.applyCh {
		if msg.CommandValid {
			op := msg.Command.(Op)
			kv.mu.Lock()
			kv.DoOperation(op)
			kv.applyIndex = msg.CommandIndex
			kv.mu.Unlock()
		} else if msg.SnapshotValid {
			var data []map[string]string
			var clientSeq []map[int64]int64
			var initialize bool
			var currShards []bool
			var tasks []moveShardTask
			var serverSeq []map[int64]int64
			reader := bytes.NewBuffer(msg.Snapshot)
			decoder := labgob.NewDecoder(reader)
			if decoder.Decode(&data) == nil &&
				decoder.Decode(&clientSeq) == nil &&
				decoder.Decode(&initialize) == nil &&
				decoder.Decode(&currShards) == nil &&
				decoder.Decode(&tasks) == nil &&
				decoder.Decode(&serverSeq) == nil {
				kv.mu.Lock()
				kv.data = data
				kv.clientSeq = clientSeq
				kv.initialize = initialize
				kv.currShards = currShards
				kv.tasks = tasks
				kv.serverSeq = serverSeq
				kv.mu.Unlock()
			}
		}
	}

}

func (kv *ShardKV) trysnapshot() {
	for {
		if kv.maxraftstate > 0 && kv.rf.RaftStateSize() > kv.maxraftstate*8/10 {
			kv.mu.Lock()
			writer := new(bytes.Buffer)
			encoder := labgob.NewEncoder(writer)
			encoder.Encode(kv.data)
			encoder.Encode(kv.clientSeq)
			encoder.Encode(kv.initialize)
			encoder.Encode(kv.currShards)
			encoder.Encode(kv.tasks)
			encoder.Encode(kv.serverSeq)

			applyindex := kv.applyIndex
			snapshot := writer.Bytes()
			kv.rf.Snapshot(applyindex, snapshot)
			kv.mu.Unlock()
		}
		time.Sleep(5 * time.Millisecond)
	}
}
```

与Lab3中相同，分片KV服务器同样需要执行`trysnapshot()`及`receiveMsg()`用于接受提交的日志及尝试压缩日志，与Lab3的区别仅为需要保存至快照的条目增多，在这里不加赘述。

```go
func (kv *ShardKV) DoOperation(op Op) {
	if op.Type == PUTSHARD {
		if kv.serverSeq[op.Shard][op.ClientId] >= op.SequenceNum {
			return
		}
		kv.serverSeq[op.Shard][op.ClientId] = op.SequenceNum
		kv.currShards[op.Shard] = true
		kv.data[op.Shard] = map[string]string{}
		kv.clientSeq[op.Shard] = map[int64]int64{}
		for k, v := range op.ShardData {
			kv.data[op.Shard][k] = v
		}
		for k, v := range op.ShardSeq {
			kv.clientSeq[op.Shard][k] = v
		}

		DPrintf("[server %d.%d] PUTSHARD done currshards = %v,data = %v, op = %v", kv.gid, kv.me, kv.currShards, kv.data, op)
	} else if op.Type == MOVETASK {
		if !kv.currShards[op.Shard] || kv.serverSeq[op.Shard][op.ClientId] >= op.SequenceNum {
			return
		}
		kv.serverSeq[op.Shard][op.ClientId] = op.SequenceNum
		var task moveShardTask
		kv.currShards[op.Shard] = false
		task.Shard = op.Shard
		task.ConfigNum = op.ConfigNum
		task.ShardData = make(map[string]string)
		task.ShardSeq = make(map[int64]int64)
		task.ShardBuffer = make(map[int64]string)
		task.Servers = op.Servers
		for k, v := range kv.data[op.Shard] {
			task.ShardData[k] = v
		}
		for k, v := range kv.clientSeq[op.Shard] {
			task.ShardSeq[k] = v
		}
		kv.data[op.Shard] = map[string]string{}
		kv.clientSeq[op.Shard] = map[int64]int64{}
		kv.tasks = append(kv.tasks, task)
		DPrintf("[server %d.%d] MOVETASK done currshards = %v", kv.gid, kv.me, kv.currShards)
	} else {
		if !kv.currShards[op.Shard] || kv.clientSeq[op.Shard][op.ClientId] >= op.SequenceNum {
			return
		}
		kv.clientSeq[op.Shard][op.ClientId] = op.SequenceNum
		if op.Type == PUT {
			kv.data[op.Shard][op.Key] = op.Val
		} else if op.Type == APPEND {
			ret := kv.data[op.Shard][op.Key]
			kv.data[op.Shard][op.Key] = ret + op.Val
		}
	}
}
```

在`DoOperation(op Op)`中，服务器对提交的日志命令进行实际操作，对于不同命令类型，其分别执行不同的操作：

1. 当收到`GET`、`APPEND`或`PUT`操作时，其判断当前服务器中是否仍存在对应分片，如不存在则直接返回。并且，服务器使用`kv.clientSeq`完成对客户端操作的去重，并记录当前已完成的操作序号，然后对于`PUT`及`APPEND`执行对应的写操作。在这里，与Lab3的不同之处为`clientSeq`及`data`均需要按分片不同进行隔离，以方便分片转移；
2. 当收到`MOVETASK`时，其同样需要判断当前服务器中是否仍存在对应分片，如不存在则直接返回。并且，服务器使用`kv.serverSeq`完成对`MOVETASK`的去重操作，其中`op.ClientId`为服务器的组ID、`op.SequenceNum`为发送任务时的配置编号。在去重操作后，服务器拷贝对应分片的数据形成分片转移任务，并将其插入`kv.tasks`；
3. 当收到`PUTSHARD`时，服务器使用`kv.serverSeq`完成去重操作，其中`op.ClientId`为发送端的组ID、`op.SequenceNum`为发送任务时的配置编号。在去重操作后，服务器将对应的分片部署至本地服务器集群。

## 实验结果

本实验中，所设计的分片转移方法可以使得分片转移在发送端的服务器集群及接受端的服务器集群中获取共同共识，以保证操作的线性一致性，通过若干次的测试用例重复执行，证明了该方法的稳健性。

```
Test: static shards ...
  ... Passed
Test: join then leave ...
  ... Passed
Test: snapshots, join, and leave ...
  ... Passed
Test: servers miss configuration changes...
  ... Passed
Test: concurrent puts and configuration changes...
  ... Passed
Test: more concurrent puts and configuration changes...
  ... Passed
Test: concurrent configuration change and restart...
  ... Passed
Test: unreliable 1...
  ... Passed
Test: unreliable 2...
  ... Passed
Test: unreliable 3...
  ... Passed
Test: shard deletion (challenge 1) ...
  ... Passed
Test: unaffected shard access (challenge 2) ...
  ... Passed
Test: partial migration shard access (challenge 2) ...
  ... Passed
PASS
ok  	6.824/shardkv	201.823s
```

