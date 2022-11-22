package kvraft

import (
	"bytes"
	"log"
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

const GET = 0
const PUT = 1
const APPEND = 2

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

//
// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
//
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//

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
		time.Sleep(10 * time.Millisecond)
	}

}
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
