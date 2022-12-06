package shardctrler

//
// Shard controler: assigns shards to replication groups.
//
// RPC interface:
// Join(servers) -- add a set of groups (gid -> server-list mapping).
// Leave(gids) -- delete a set of groups.
// Move(shard, gid) -- hand off one shard from current owner to gid.
// Query(num) -> fetch Config # num, or latest config if num==-1.
//
// A Config (configuration) describes a set of replica groups, and the
// replica group responsible for each shard. Configs are numbered. Config
// #0 is the initial configuration, with no groups and all shards
// assigned to group 0 (the invalid group).
//
// You will need to add fields to the RPC argument structs.
//

// The number of shards.
const NShards = 10

// A configuration -- an assignment of shards to groups.
// Please don't change this.
type Config struct {
	Num    int              // config number
	Shards [NShards]int     // shard -> gid
	Groups map[int][]string // gid -> servers[]
}

const (
	OK = "OK"
)

type Err string

type JoinArgs struct {
	Servers     map[int][]string // new GID -> servers mappings
	ClientId    int64
	SequenceNum int64
}

type JoinReply struct {
	Done bool
	Err  Err
}

type LeaveArgs struct {
	GIDs        []int
	ClientId    int64
	SequenceNum int64
}

type LeaveReply struct {
	Done bool
	Err  Err
}

type MoveArgs struct {
	Shard       int
	GID         int
	ClientId    int64
	SequenceNum int64
}

type MoveReply struct {
	Done bool
	Err  Err
}

type QueryArgs struct {
	Num         int // desired config number
	ClientId    int64
	SequenceNum int64
}

type QueryReply struct {
	Done   bool
	Err    Err
	Config Config
}

type GetShardArgs struct {
	Num         int // desired config number
	ClientId    int64
	SequenceNum int64
}

type GetShardReply struct {
	Done   bool
	Err    Err
	Config Config
}
