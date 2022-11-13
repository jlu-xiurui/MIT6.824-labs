package kvraft

import (
	"crypto/rand"
	"math/big"
	"sync/atomic"

	"6.824/labrpc"
)

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

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler functfalseon's
// arguments. and reply must be passed as a pointer.
//
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
	// You will have to modify this function.
}

//
// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
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

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}
