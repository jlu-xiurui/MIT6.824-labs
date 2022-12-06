package shardkv

import (
	"bytes"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"6.824/labgob"
	"6.824/labrpc"
	"6.824/raft"
	"6.824/shardctrler"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

const GET = 0
const PUT = 1
const APPEND = 2
const PUTSHARD = 3
const MOVETASK = 4

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
type moveShardTask struct {
	ConfigNum   int
	Shard       int
	ShardData   map[string]string
	ShardSeq    map[int64]int64
	ShardBuffer map[int64]string
	Servers     []string
}
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Type        int // 0 - Get, 1 - Put, 2 - Append, 3 - UpdateShard, 4 - NewConfig
	Key         string
	Val         string
	ClientId    int64
	SequenceNum int64
	ConfigNum   int
	Shard       int
	ShardData   map[string]string
	ShardSeq    map[int64]int64
	ShardBuffer map[int64]string
	Servers     []string
}

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
			DPrintf("[server %d.%d] UpdateConfig shards = %v,currshards = %v,needshards = %v,isleader = %t", kv.gid, kv.me, kv.config.Shards, kv.currShards, kv.needShards, isleader)
		}
		for i := range kv.currShards {
			if !kv.needShards[i] && kv.currShards[i] {
				_, _, ok := kv.rf.Start(Op{Type: MOVETASK, ConfigNum: kv.config.Num, Shard: i,
					ClientId: int64(kv.gid), SequenceNum: int64(kv.config.Num), Servers: kv.config.Groups[kv.config.Shards[i]]})
				if ok {
					DPrintf("[server %d.%d] Start MOVETASK op : %v", kv.gid, kv.me, Op{Type: MOVETASK, ConfigNum: kv.config.Num, Shard: i,
						ClientId: int64(kv.gid), SequenceNum: int64(kv.config.Num), Servers: kv.config.Groups[kv.config.Shards[i]]})
				}
			}
		}
		kv.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}
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
func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.

	kv.mu.Lock()
	DPrintf("[server %d.%d] Get : clientId : %d,seq : %d,Shard : %d,Gid = %d,key : %s", kv.gid, kv.me, args.ClientId, args.SequenceNum, args.Shard, args.Gid, args.Key)
	if kv.config.Shards[args.Shard] != kv.gid || !kv.currShards[args.Shard] {
		DPrintf("[server %d.%d] Get WRONG GROUP:Shard : %d,Gid = %d,configNum = %d,currShards =  %v", kv.gid, kv.me, args.Shard, args.Gid, kv.config.Num, kv.currShards)
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
			DPrintf("[server %d.%d] Get over: clientId : %d,seq : %d,key : %s", kv.gid, kv.me, args.ClientId, args.SequenceNum, args.Key)
			reply.Value = kv.data[args.Shard][args.Key]
			//delete(kv.readBuffer[args.ClientId], args.SequenceNum)
			kv.mu.Unlock()
			reply.Err = OK
			return
		}
		kv.mu.Unlock()
	}

}

func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.

	kv.mu.Lock()
	DPrintf("[server %d.%d] PutAppend : clientId : %d,seq : %d,Shard : %d,Gid = %d,key : %s, val : %s, Op : %s", kv.gid, kv.me, args.ClientId,
		args.SequenceNum, args.Shard, args.Gid, args.Key, args.Value, args.Op)
	if kv.config.Shards[args.Shard] != kv.gid || !kv.currShards[args.Shard] {
		DPrintf("[server %d.%d] Put WRONG GROUP:Shard : %d,Gid = %d,configNum = %d,currShards =  %v", kv.gid, kv.me, args.Shard, args.Gid, kv.config.Num, kv.currShards)
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
	_, _, isleader := kv.rf.Start(Op{Type: opType, Key: args.Key, Val: args.Value, ClientId: args.ClientId, SequenceNum: args.SequenceNum, Shard: args.Shard})
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
			DPrintf("[server %d.%d] PutAppend over: clientId : %d,seq : %d,key : %s, val : %s, Op : %s", kv.gid, kv.me, args.ClientId, args.SequenceNum, args.Key, args.Value, args.Op)
			kv.mu.Unlock()
			reply.Err = OK
			return
		}
		kv.mu.Unlock()
	}
}

//
// the tester calls Kill() when a ShardKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *ShardKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
}

//
// servers[] contains the ports of the servers in this group.
//
// me is the index of the current server in servers[].
//
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
//
// the k/v server should snapshot when Raft's saved state exceeds
// maxraftstate bytes, in order to allow Raft to garbage-collect its
// log. if maxraftstate is -1, you don't need to snapshot.
//
// gid is this group's GID, for interacting with the shardctrler.
//
// pass ctrlers[] to shardctrler.MakeClerk() so you can send
// RPCs to the shardctrler.
//
// make_end(servername) turns a server name from a
// Config.Groups[gid][i] into a labrpc.ClientEnd on which you can
// send RPCs. You'll need this to send RPCs to other groups.
//
// look at client.go for examples of how to use ctrlers[]
// and make_end() to send RPCs to the group owning a specific shard.
//
// StartServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func (kv *ShardKV) receiveMsg() {
	for msg := range kv.applyCh {

		if msg.CommandValid {
			//DPrintf("[server %d.%d],msg receive cmd %v,raftstatesize = %d", kv.gid, kv.me, msg, kv.rf.RaftStateSize())
			op := msg.Command.(Op)
			kv.mu.Lock()
			kv.DoOperation(op)
			kv.applyIndex = msg.CommandIndex
			kv.mu.Unlock()
		} else if msg.SnapshotValid {
			DPrintf("[server %d.%d],msg receive snapshot %d,raftstatesize = %d", kv.gid, kv.me, msg.SnapshotIndex, kv.rf.RaftStateSize())
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
	// Your initialization code here.

	// Use something like this to talk to the shardctrler:
	// kv.mck = shardctrler.MakeClerk(kv.ctrlers)

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh, "KV-"+strconv.Itoa(gid))
	kv.data = make([]map[string]string, kv.NShards)
	kv.clientSeq = make([]map[int64]int64, kv.NShards)
	kv.currShards = make([]bool, kv.NShards)
	kv.needShards = make([]bool, kv.NShards)
	kv.serverSeq = make([]map[int64]int64, kv.NShards)
	for i := 0; i < kv.NShards; i++ {
		kv.data[i] = make(map[string]string)
		kv.clientSeq[i] = make(map[int64]int64)
		kv.serverSeq[i] = make(map[int64]int64)
	}
	var data []map[string]string
	var clientSeq []map[int64]int64
	var initialize bool
	var currShards []bool
	var tasks []moveShardTask
	var serverSeq []map[int64]int64
	snapshot := persister.ReadSnapshot()
	reader := bytes.NewBuffer(snapshot)
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
	go kv.updateConfig()
	go kv.receiveMsg()
	go kv.trysnapshot()
	go kv.MoveShard()
	DPrintf("[server %d.%d] Start", kv.me, kv.gid)
	return kv
}
