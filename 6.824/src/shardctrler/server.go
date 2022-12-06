package shardctrler

import (
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"6.824/labgob"
	"6.824/labrpc"
	"6.824/raft"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

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

const (
	JOIN      = 0
	LEAVE     = 1
	MOVE      = 2
	QUERY     = 3
	ASKSHARDS = 4
)

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

func (sc *ShardCtrler) Join(args *JoinArgs, reply *JoinReply) {
	// Your code here.
	_, _, isleader := sc.rf.Start(Op{Type: JOIN, Servers: args.Servers, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
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

func (sc *ShardCtrler) Leave(args *LeaveArgs, reply *LeaveReply) {
	// Your code here.
	_, _, isleader := sc.rf.Start(Op{Type: LEAVE, GIDs: args.GIDs, ClientId: args.ClientId, SequenceNum: args.SequenceNum})
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

//
// the tester calls Kill() when a ShardCtrler instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (sc *ShardCtrler) Kill() {
	sc.rf.Kill()
	// Your code here, if desired.
}

// needed by shardsc tester
func (sc *ShardCtrler) Raft() *raft.Raft {
	return sc.rf
}
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
func (sc *ShardCtrler) receiveMsg() {
	for msg := range sc.applyCh {
		op := msg.Command.(Op)
		sc.mu.Lock()
		DPrintf("[server %d],msg receive %v,", sc.me, msg)
		sc.DoOperation(op)
		sc.mu.Unlock()
	}

}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant shardctrler service.
// me is the index of the current server in servers[].
//
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
