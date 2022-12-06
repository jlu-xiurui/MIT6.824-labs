package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"6.824/labgob"
	"6.824/labrpc"
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
//
const LEADER = 1
const CANDIDATE = 2
const FOLLOWER = 3

const BROADCASTTIME = 100
const ELECTIONTIMEOUTBASE = 1000
const ELECTIONTIMEOUTRANGE = 1000

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type LogEntry struct {
	Command interface{}
	Term    int
	Index   int
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu           sync.Mutex          // Lock to protect shared access to this peer's state
	peers        []*labrpc.ClientEnd // RPC end points of all peers
	persister    *Persister          // Object to hold this peer's persisted state
	me           int                 // this peer's index into peers[]
	dead         int32               // set by Kill()
	applych      chan ApplyMsg
	cond         *sync.Cond
	quicklyCheck int32
	name         string
	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	BroadcastTime   int
	ElectionTimeout int
	State           int // 1 - leader, 2 - candidate, 3 - follower
	CurrentTerm     int
	VotedFor        int
	Log             []LogEntry
	CommitIndex     int
	LastApplied     int
	NextIndex       []int
	MatchIndex      []int

	LastIncludedIndex int
	LastIncludedTerm  int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) RaftStateSize() int {
	return rf.persister.RaftStateSize()
}
func (rf *Raft) GetState() (int, bool) {
	if rf.killed() {
		return -1, false
	}
	var term int
	var isleader bool
	rf.mu.Lock()
	// Your code here (2A).
	term = rf.CurrentTerm
	isleader = rf.State == LEADER
	rf.mu.Unlock()
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	writer := new(bytes.Buffer)
	encoder := labgob.NewEncoder(writer)
	encoder.Encode(rf.CurrentTerm)
	encoder.Encode(rf.VotedFor)
	encoder.Encode(rf.Log)
	encoder.Encode(rf.LastIncludedIndex)
	encoder.Encode(rf.LastIncludedTerm)
	data := writer.Bytes()
	rf.persister.SaveRaftState(data)
	rf.DPrintf("[%d] call Persist(),rf.CurrentTerm = %d,rf.VotedFor = %d,rf.Log len = %d,lastIncludedIndex = %d", rf.me, rf.CurrentTerm, rf.VotedFor, len(rf.Log), rf.LastIncludedIndex)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reader := bytes.NewBuffer(data)
	decoder := labgob.NewDecoder(reader)
	var currentTerm int
	var votedFor int
	var log []LogEntry
	var lastIncludedIndex int
	var lastIncludedTerm int
	if decoder.Decode(&currentTerm) != nil ||
		decoder.Decode(&votedFor) != nil ||
		decoder.Decode(&log) != nil ||
		decoder.Decode(&lastIncludedIndex) != nil ||
		decoder.Decode(&lastIncludedTerm) != nil {
		panic("readPersist : decode err")
	} else {
		rf.CurrentTerm = currentTerm
		rf.VotedFor = votedFor
		rf.Log = log
		rf.LastIncludedIndex = lastIncludedIndex
		rf.LastIncludedTerm = lastIncludedTerm
		rf.DPrintf("[%d] call readPersist(),rf.CurrentTerm = %d,rf.VotedFor = %d,rf.Log len = %d,lastIncludedIndex = %d", rf.me, rf.CurrentTerm, rf.VotedFor, len(rf.Log), lastIncludedIndex)
	}
}

//
// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	rf.mu.Lock()
	if index <= rf.LastIncludedIndex {
		rf.mu.Unlock()
		return
	}
	rf.DPrintf("[%d] CALL Snapshot index = %d", rf.me, index)
	rf.LastIncludedTerm = rf.GetLogIndex(index).Term
	var log []LogEntry
	for i := index + 1; i <= rf.GetLastEntry().Index; i++ {
		log = append(log, rf.GetLogIndex(i))
	}
	rf.Log = log
	rf.LastIncludedIndex = index

	writer := new(bytes.Buffer)
	encoder := labgob.NewEncoder(writer)
	encoder.Encode(rf.CurrentTerm)
	encoder.Encode(rf.VotedFor)
	encoder.Encode(rf.Log)
	encoder.Encode(rf.LastIncludedIndex)
	encoder.Encode(rf.LastIncludedTerm)
	data := writer.Bytes()
	rf.persister.SaveStateAndSnapshot(data, snapshot)
	rf.mu.Unlock()
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
	XTerm   int
	XIndex  int
	XLen    int
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Snapshot          []byte
}

type InstallSnapshotReply struct {
	Term int
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).

	rf.mu.Lock()
	//rf.DPrintf("[%d] Get RequestVote from %d", rf.me, args.CandidateId)
	defer rf.mu.Unlock()
	reply.Term = rf.CurrentTerm
	LastEntry := rf.GetLastEntry()
	LastIndex := LastEntry.Index
	LastTerm := LastEntry.Term
	if rf.CurrentTerm > args.Term {
		reply.VoteGranted = false
		return
	} else if rf.CurrentTerm < args.Term {
		rf.State = FOLLOWER
		rf.CurrentTerm = args.Term
		rf.VotedFor = -1
		rf.persist()
	}
	if rf.VotedFor == -1 && (LastTerm < args.LastLogTerm || (LastTerm == args.LastLogTerm && LastIndex <= args.LastLogIndex)) {
		reply.VoteGranted = true
		rf.VotedFor = args.CandidateId
		rf.persist()
		rf.ElectionTimeout = GetElectionTimeout()
		rf.DPrintf("[%d %d] VoteFor %d(term : %d)\n", rf.me, rf.State, args.CandidateId, rf.CurrentTerm)
	}
}
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {

	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.DPrintf("[%d %d] get AppendEntries from %d, LastIncludeIndex = %d,currterm = %d(PrevLogIndex:%d,PrevLogTerm:%d,Leaderterm:%d)\n",
		rf.me, rf.State, args.LeaderId, rf.LastIncludedIndex, rf.CurrentTerm, args.PrevLogIndex, args.PrevLogTerm, args.Term)
	reply.Term = rf.CurrentTerm
	reply.Success = true
	rf.ElectionTimeout = GetElectionTimeout()
	if args.Term < rf.CurrentTerm || rf.LastIncludedIndex > args.PrevLogIndex {
		reply.Success = false
		return
	}
	if rf.CurrentTerm < args.Term || rf.State == CANDIDATE {
		rf.State = FOLLOWER
		rf.CurrentTerm = args.Term
		rf.VotedFor = -1
		rf.cond.Broadcast()
		rf.persist()
	}
	if rf.GetLastEntry().Index < args.PrevLogIndex || args.PrevLogTerm != rf.GetLogIndex(args.PrevLogIndex).Term {
		reply.XLen = rf.GetLastEntry().Index
		if rf.GetLastEntry().Index >= args.PrevLogIndex {
			reply.XTerm = rf.GetLogIndex(args.PrevLogIndex).Term
			reply.XIndex = args.PrevLogIndex
			for reply.XIndex > rf.LastIncludedIndex && rf.GetLogIndex(reply.XIndex).Term == reply.XTerm {
				reply.XIndex--
			}
			reply.XIndex++
		}
		rf.DPrintf("[%d %d] AppendEntries fail because of consistence, XLen = %d, XTerm = %d, XIndex = %d", rf.me, rf.State, reply.XLen, reply.XTerm, reply.XIndex)
		reply.Success = false
		return
	}
	for index, entry := range args.Entries {
		if rf.GetLastEntry().Index < entry.Index || entry.Term != rf.GetLogIndex(entry.Index).Term {
			var log []LogEntry
			for i := rf.LastIncludedIndex + 1; i <= entry.Index-1; i++ {
				log = append(log, rf.GetLogIndex(i))
			}
			log = append(log, args.Entries[index:]...)
			rf.Log = log
			rf.persist()
			rf.DPrintf("[%d %d] Append new log %v", rf.me, rf.State, rf.Log)
		}
	}
	if args.LeaderCommit > rf.CommitIndex {
		rf.CommitIndex = Min(args.LeaderCommit, rf.GetLastEntry().Index)
		rf.DPrintf("[%d %d] CommitIndex update to %d\n", rf.me, rf.State, rf.CommitIndex)
	}
}
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	reply.Term = rf.CurrentTerm
	rf.ElectionTimeout = GetElectionTimeout()
	if args.Term < rf.CurrentTerm || args.LastIncludedIndex < rf.LastIncludedIndex {
		rf.mu.Unlock()
		return
	}
	if rf.CurrentTerm < args.Term || rf.State == CANDIDATE {
		rf.State = FOLLOWER
		rf.CurrentTerm = args.Term
		rf.VotedFor = -1
		rf.cond.Broadcast()
		rf.persist()
	}
	writer := new(bytes.Buffer)
	encoder := labgob.NewEncoder(writer)
	encoder.Encode(rf.CurrentTerm)
	encoder.Encode(rf.VotedFor)
	encoder.Encode(rf.Log)
	encoder.Encode(rf.LastIncludedIndex)
	encoder.Encode(rf.LastIncludedTerm)
	data := writer.Bytes()
	rf.persister.SaveStateAndSnapshot(data, args.Snapshot)

	rf.LastIncludedIndex = args.LastIncludedIndex
	rf.LastIncludedTerm = args.LastIncludedTerm
	LastEntry := rf.GetLastEntry()
	LastIndex := LastEntry.Index
	if args.LastIncludedIndex < LastIndex {
		entry := rf.GetLogIndex(args.LastIncludedIndex)
		if entry.Term == args.LastIncludedTerm {
			var log []LogEntry
			for i := args.LastIncludedIndex + 1; i <= LastIndex; i++ {
				log = append(log, rf.GetLogIndex(i))
			}
			rf.Log = log
			rf.persist()
			rf.DPrintf("[%d] InstallSnapShot success,LastIncludeIndex = %d\n", rf.me, rf.LastIncludedIndex)
			rf.mu.Unlock()
			rf.applych <- ApplyMsg{SnapshotValid: true, Snapshot: args.Snapshot, SnapshotTerm: args.LastIncludedTerm, SnapshotIndex: args.LastIncludedIndex}
			rf.DPrintf("[%d] apply snapshot,LastIncludeIndex = %d\n", rf.me, rf.LastIncludedIndex)
			return
		}
	}
	rf.Log = make([]LogEntry, 0)
	rf.persist()
	rf.DPrintf("[%d] InstallSnapShot success,LastIncludeIndex = %d\n", rf.me, rf.LastIncludedIndex)
	rf.mu.Unlock()
	rf.applych <- ApplyMsg{SnapshotValid: true, Snapshot: args.Snapshot, SnapshotTerm: args.LastIncludedTerm, SnapshotIndex: args.LastIncludedIndex}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.rue
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}
func (rf *Raft) SendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	_, isLeader := rf.GetState()
	if !isLeader || rf.killed() {
		rf.DPrintf("[%d] Start() Fail isleader = %t, isKilled = %t", rf.me, isLeader, rf.killed())
		return -1, -1, false
	}
	// Your code here (2B).
	//atomic.StoreInt32(&rf.applying, 1)
	rf.mu.Lock()
	logEntry := LogEntry{Command: command, Term: rf.CurrentTerm, Index: rf.GetLastEntry().Index + 1}
	rf.Log = append(rf.Log, logEntry)
	rf.persist()
	rf.mu.Unlock()
	rf.DPrintf("[%d] Start() ,index : %d,term : %d,command : %d", rf.me, logEntry.Index, logEntry.Term, command)
	atomic.StoreInt32(&rf.quicklyCheck, 20)
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go rf.SendEntries(i)
		}

	}
	return logEntry.Index, logEntry.Term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}
func (rf *Raft) doElection() {
	rf.CurrentTerm++
	voteGranted := 1
	rf.VotedFor = rf.me
	rf.persist()
	rf.ElectionTimeout = GetElectionTimeout()
	term := rf.CurrentTerm
	ElectionTimeout := rf.ElectionTimeout
	var lastLogTerm, lastLogIndex int
	lastLogIndex = rf.GetLastEntry().Index
	lastLogTerm = rf.GetLastEntry().Term
	rf.DPrintf("[%d] Start Election,term = %d\n", rf.me, term)
	rf.mu.Unlock()
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go func(server int) {
				args := RequestVoteArgs{Term: term, CandidateId: rf.me, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm}
				reply := RequestVoteReply{}
				rf.DPrintf("[%d] send RequestVote to server %d\n", rf.me, server)
				if !rf.sendRequestVote(server, &args, &reply) {
					rf.cond.Broadcast()
					return
				}
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.VoteGranted {
					voteGranted++
				}
				if reply.Term > rf.CurrentTerm {
					rf.CurrentTerm = reply.Term
					rf.persist()
					rf.State = FOLLOWER
					rf.ElectionTimeout = GetElectionTimeout()
				}
				rf.cond.Broadcast()
			}(i)
		}
	}
	var timeout int32
	go func(electionTimeout int, timeout *int32) {
		time.Sleep(time.Duration(electionTimeout) * time.Millisecond)
		atomic.StoreInt32(timeout, 1)
		rf.cond.Broadcast()

	}(ElectionTimeout, &timeout)
	for {
		rf.mu.Lock()
		if voteGranted <= len(rf.peers)/2 && rf.CurrentTerm == term && rf.State == CANDIDATE && atomic.LoadInt32(&timeout) == 0 {
			rf.cond.Wait()
		}
		if rf.CurrentTerm != term || rf.State != CANDIDATE || atomic.LoadInt32(&timeout) != 0 {
			rf.mu.Unlock()
			break
		}
		if voteGranted > len(rf.peers)/2 {
			rf.DPrintf("[%d] is voted as LEADER(term : %d)\n", rf.me, rf.CurrentTerm)
			rf.State = LEADER
			rf.CommitIndex = 0
			for i := 0; i < len(rf.peers); i++ {
				rf.MatchIndex[i] = 0
				rf.NextIndex[i] = rf.GetLastEntry().Index + 1
			}
			rf.mu.Unlock()
			rf.TrySendEntries(true)
			break
		}
		rf.mu.Unlock()
	}
	rf.DPrintf("[%d] Do election finished", rf.me)

}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) SendSnapshot(server int) {
	rf.mu.Lock()
	term := rf.CurrentTerm
	leaderId := rf.me
	lastIncludedIndex := rf.LastIncludedIndex
	lastIncludedTerm := rf.LastIncludedTerm
	snapshot := rf.persister.snapshot
	args := InstallSnapshotArgs{Term: term, LeaderId: leaderId, LastIncludedIndex: lastIncludedIndex, LastIncludedTerm: lastIncludedTerm, Snapshot: snapshot}
	reply := InstallSnapshotReply{}
	rf.DPrintf("[%d] send SnapShot to server %d,LastIncludeIndex = %d\n", rf.me,
		server, rf.LastIncludedIndex)
	rf.mu.Unlock()
	if !rf.SendInstallSnapshot(server, &args, &reply) {
		return
	}
	rf.mu.Lock()
	if reply.Term > rf.CurrentTerm {
		rf.CurrentTerm = reply.Term
		rf.State = FOLLOWER
		rf.ElectionTimeout = GetElectionTimeout()
		rf.VotedFor = -1
		rf.persist()
	}
	rf.NextIndex[server] = rf.LastIncludedIndex + 1
	rf.MatchIndex[server] = rf.LastIncludedIndex
	rf.mu.Unlock()

}
func (rf *Raft) SendHeartBeat(server int) {
	rf.mu.Lock()
	rf.DPrintf("[%d] send heartBeat to server %d\n", rf.me, server)
	if rf.State != LEADER {
		rf.mu.Unlock()
		return
	}
	if rf.NextIndex[server] <= rf.LastIncludedIndex {
		rf.mu.Unlock()
		return
	}
	term := rf.CurrentTerm
	leaderCommit := rf.CommitIndex
	prevLogIndex := rf.NextIndex[server] - 1
	prevLogTerm := rf.GetLogIndex(prevLogIndex).Term
	rf.mu.Unlock()
	args := AppendEntriesArgs{Term: term, LeaderId: rf.me, LeaderCommit: leaderCommit, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm}
	reply := AppendEntriesReply{}
	if !rf.sendAppendEntries(server, &args, &reply) {
		return
	}
	rf.mu.Lock()
	if reply.Term > rf.CurrentTerm {
		rf.CurrentTerm = reply.Term
		rf.State = FOLLOWER
		rf.ElectionTimeout = GetElectionTimeout()
		rf.VotedFor = -1
		rf.persist()
		rf.mu.Unlock()
		return
	}
	if !reply.Success {
		if reply.XLen < prevLogIndex {
			rf.NextIndex[server] = Max(reply.XLen, 1)
		} else {
			newNextIndex := prevLogIndex
			for newNextIndex > rf.LastIncludedIndex && rf.GetLogIndex(newNextIndex).Term > reply.XTerm {
				newNextIndex--
			}
			if rf.GetLogIndex(newNextIndex).Term == reply.XTerm {
				rf.NextIndex[server] = Max(newNextIndex, rf.LastIncludedIndex+1)
			} else {
				rf.NextIndex[server] = reply.XIndex
			}
		}
		rf.DPrintf("[%d] sendheartbeat rf.NextIndex[%d] update to %d", rf.me, server, rf.NextIndex[server])
	}
	rf.mu.Unlock()
}
func (rf *Raft) SendEntries(server int) {
	done := false
	for !done {
		rf.mu.Lock()
		if rf.State != LEADER {
			rf.mu.Unlock()
			return
		}
		if rf.NextIndex[server] <= rf.LastIncludedIndex {
			rf.mu.Unlock()
			return
		}
		done = true
		term := rf.CurrentTerm
		leaderCommit := rf.CommitIndex
		prevLogIndex := rf.NextIndex[server] - 1
		prevLogTerm := rf.GetLogIndex(prevLogIndex).Term
		entries := rf.Log[prevLogIndex-rf.LastIncludedIndex:]
		rf.DPrintf("[%d] send Entries to server %d,prevLogIndex = %d,prevLogTerm = %d,LastIncludeIndex = %d\n", rf.me,
			server, prevLogIndex, prevLogTerm, rf.LastIncludedIndex)
		rf.mu.Unlock()
		args := AppendEntriesArgs{Term: term, LeaderId: rf.me, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm, Entries: entries, LeaderCommit: leaderCommit}
		reply := AppendEntriesReply{}
		if !rf.sendAppendEntries(server, &args, &reply) {
			return
		}
		rf.mu.Lock()
		if reply.Term > rf.CurrentTerm {
			rf.CurrentTerm = reply.Term
			rf.State = FOLLOWER
			rf.ElectionTimeout = GetElectionTimeout()
			rf.VotedFor = -1
			rf.persist()
			rf.mu.Unlock()
			return
		}
		if !reply.Success {
			//if reply.LastIncludedIndex > prevLogIndex {
			//	rf.NextIndex[server] = reply.LastIncludedIndex + 1
			//}
			if reply.XLen < prevLogIndex {
				rf.NextIndex[server] = Max(reply.XLen, 1)
			} else {
				newNextIndex := prevLogIndex
				for newNextIndex > rf.LastIncludedIndex && rf.GetLogIndex(newNextIndex).Term > reply.XTerm {
					newNextIndex--
				}
				if rf.GetLogIndex(newNextIndex).Term == reply.XTerm {
					rf.NextIndex[server] = Max(newNextIndex, rf.LastIncludedIndex+1)
				} else {
					rf.NextIndex[server] = reply.XIndex
				}
			}
			rf.DPrintf("[%d] sendentires rf.NextIndex[%d] update to %d", rf.me, server, rf.NextIndex[server])
			done = false
		} else {
			rf.NextIndex[server] = Max(rf.NextIndex[server], prevLogIndex+len(entries)+1)
			rf.MatchIndex[server] = Max(rf.MatchIndex[server], prevLogIndex+len(entries))
			rf.DPrintf("[%d] AppendEntries success,NextIndex is %v,MatchIndex is %v", rf.me, rf.NextIndex, rf.MatchIndex)
		}
		rf.mu.Unlock()
	}
}
func (rf *Raft) TrySendEntries(initialize bool) {
	for i := 0; i < len(rf.peers); i++ {
		rf.mu.Lock()
		nextIndex := rf.NextIndex[i]
		firstLogIndex := rf.GetFirstEntry().Index
		lastLogIndex := rf.GetLastEntry().Index
		rf.mu.Unlock()
		if i != rf.me {
			if lastLogIndex >= nextIndex || initialize {
				if firstLogIndex <= nextIndex {
					go rf.SendEntries(i)
				} else {
					go rf.SendSnapshot(i)
				}
			} else {
				go rf.SendHeartBeat(i)
			}
		}
	}
}
func (rf *Raft) UpdateCommitIndex() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	newCommitIndex := rf.CommitIndex
	for N := rf.CommitIndex + 1; N <= rf.GetLastEntry().Index; N++ {
		if N > rf.LastIncludedIndex && rf.GetLogIndex(N).Term == rf.CurrentTerm {
			count := 1
			for i := 0; i < len(rf.peers); i++ {
				if rf.MatchIndex[i] >= N {
					count++
					if count > len(rf.peers)/2 {
						newCommitIndex = N
						break
					}
				}
			}
		}
	}
	rf.CommitIndex = newCommitIndex
	rf.DPrintf("[%d] Update CommitIndex, term = %d,NextIndex is %v,MatchIndex is %v,CommitIndex is %d", rf.me, rf.CurrentTerm, rf.NextIndex, rf.MatchIndex, rf.CommitIndex)
}
func (rf *Raft) UpdateApplied() {
	rf.LastApplied = Max(rf.LastApplied, rf.LastIncludedIndex)
	for rf.LastApplied < rf.CommitIndex && rf.LastApplied < rf.GetLastEntry().Index && rf.LastApplied >= rf.LastIncludedIndex {
		msg := ApplyMsg{CommandValid: true, Command: rf.GetLogIndex(rf.LastApplied + 1).Command, CommandIndex: rf.LastApplied + 1}
		rf.DPrintf("[%d] try apply msg %d", rf.me, rf.LastApplied+1)
		rf.mu.Unlock()
		rf.applych <- msg
		rf.mu.Lock()
		rf.DPrintf("[%d] apply msg %d success", rf.me, rf.LastApplied+1)
		rf.LastApplied++
	}
}
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
			rf.DPrintf("[%d] ElectionTimeout = %d", rf.me, rf.ElectionTimeout)
			if rf.ElectionTimeout < rf.BroadcastTime {
				rf.State = CANDIDATE
				rf.DPrintf("[%d] ElectionTimeout,convert to CANDIDATE\n", rf.me)
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

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg, name string) *Raft {
	rf := &Raft{}
	rf.mu.Lock()
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applych = applyCh
	rf.name = name
	rf.cond = sync.NewCond(&rf.mu)
	rf.DPrintf("[%d] is Making , len(peers) = %d\n", me, len(peers))
	// Your initialization code here (2A, 2B, 2C).
	rf.BroadcastTime = BROADCASTTIME
	rf.ElectionTimeout = GetElectionTimeout()
	rf.State = FOLLOWER
	rf.CurrentTerm = 0
	rf.VotedFor = -1
	rf.MatchIndex = make([]int, len(peers))
	rf.NextIndex = make([]int, len(peers))
	rf.mu.Unlock()
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
