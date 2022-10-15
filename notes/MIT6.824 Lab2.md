# MIT6.824 Lab2

在本实验中，需要实现Raft机制，其为一种复制状态机协议，用于实现分布式系统间的共识。Raft将客户端请求组织成一个序列，称为日志，Raft保证所有副本服务器看到相同的日志，并按日志顺序执行客户端请求，并将它们应用到服务器的本地副本，因此，所有副本服务器将拥有相同的服务状态。

## Raft原理概述

Raft由一组日志服务器组成，在工作时，Raft选举出一个“杰出的”领导者，并让领导者完全承担复制日志的责任。领导者接受来自客户端的日志条目，并将它们复制到其他服务器上，并通知其他服务器何时将哪些日志条目应用到其状态机上时安全的。

![Lab2_1](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_1.png)

如上图所示，Raft将时间分成若干不同长度的任期（term），每个任期均以选举开始，当一个候选者赢得了选举时，它将在剩下的任期内担任领导者，如果在一次选举中选票被分裂，则将没有候选者会获得选举胜利，此时则应增加term数并开展新的选举。Raft协议保证，同一term下仅有一个有效的领导者。

不同的服务器可能在不同的时间内观察到term的转换，在服务器通信时，term数将在服务器间被交换。当一个服务器的接收到大于其当前term的服务器的请求时，其会把term数更新为更大的值，并立即恢复到追随者的状态；当一个服务器接到小于其当前term的服务器请求时，其将拒绝请求。

为了实现**领导方法**，Raft可以分为三个独立的子问题：

1. 领导者选取：当Raft系统被初始化时或原有领导者奔溃时，必须选择新的领导者；
2. 日志复制：领导者需要负责接受客户端的日志条目，并将其发送到Raft集群中的其他服务器，当日志冲突时，领导者将强迫其他服务器遵从领导者的日志；
3. 安全性：Raft的共识安全性属性可以被如下描述：如果任何服务器对其状态机应用了特定的日志条目，则其他任何服务器都不能对同一日志索引应用不同的命令。这一属性将通过对领导者选取和日志复制进行约束而实现，也作为Raft中的最强安全性属性，被称为**状态机安全属性**。

### 领导者选取

一个Raft集群包含多个服务器，并可以容忍集群中半数以下的服务器发生故障。在Raft集群中，每个服务器处于三种状态之一：领导者(leader)、追随者(follower)或候选者(candidate)。在正常情况下，集群中只有一个领导者，其他服务器均为追随者，且追随者仅回应来自领导者和候选者的请求。当集群中不存在有效领导者时，即需要进行**领导者选取**。

Raft使用心跳机制来触发**领导者**选取，当服务器加入Raft集群时，其首先以**追随者**身份开始，只要其不断接收到来自服务器的有效RPC请求，其就保持追随者身份。当追随者在一定时间（选举超时时间）内没有接收到有效RPC，其将转换为**候选者**身份并开始选举试图称为领导者。为了避免追随者发起选举，领导者需要定时发送心跳RPC以告知领导者，直到其崩溃或从网络中断开。

![Lab2_2](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_2.png)

当服务器开始选举时，追随者将增加其当前term，并转换成候选者。然后，它将为自己投票，并且向集群中的其他服务器并行的发送请求选票RPC请求（`RequestVote`），直到三件事中的一件发生：（1）它获得了大多数选票，赢得选取；（2）另一个服务器赢得选取；（3）在**选举超时时间**内仍未取得胜利。

当服务器赢得选取时，它将立即向其他服务器发送心跳RPC，以防止新的选举发生。在等待`RequestVote`返回时，候选者可能受到来自其他服务器的`AppendEntries`RPC请求（心跳RPC或日志复制RPC），如果发送该请求的领导者term至少与候选者的term一样大，则候选者承认领导者是合法的，并返回到追随者状态。

在这里，为了尽可能避免分裂选票的情况，Raft将使用随机的选举超时时间，使得不同Raft开始选举的时间尽量相互错开。Raft中有两个地方将用到选举超时时间：

1. 当候选者开始选举时，其将在**选举超时时间**内等待其他服务器的投票结果，直到其获得胜利或等待超过**选举超时时间**；
2. 当服务器转换为追随者状态时，其将更新其**选举超时时间**，如服务器在超过**选举超时时间**的时间内未接受到有效RPC请求，则开始选举。如接受到有效RPC，则重新更新**选举超时时间**。

### 日志复制

一旦领导者被选举出，其就会为客户端的请求提供服务。每个客户端的请求都包括一个状态机执行命令，领导者将该命令作为要给新条目复制在其日志中，然后并行的向其他服务器发送`AppendEntries`RPC请求以复制该条目。当条目被安全复制时，领导者将会使该条目应用到本地状态机，并向其他跟随者发送条目可更新的信息。

![Lab2_3](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_3.png)

如上图所示，在Raft中，每个日志条目都具有一个index数及term数，term数用于检测日志中的不一致性（下文描述）、index数则确定了日志条目在日志中的位置。当领导者将一个日志条目在大多数服务器上成功复制时，提交该条目将被视作为安全的，这种条目在Raft中被称为**提交**的条目（具体证明过程可详见论文）。

在Raft中将维护以下属性，以保证不同服务器上日志的一致性，下述属性共同组成Raft的**日志匹配属性**：

1. 如两个不同日志中的日志条目具有相同的index和term数，其将保存相同的命令；
2. 如两个不同日志中的日志条目具有相同的index和term数，其之前的所有日志条目所组成的命令是等价的。

由于一个领导者在给定的term中只能创建一个具有给定日志索引的条目，并且Raft中只能存在一个有效的领导者，因此第一条属性是自然生效的。为了实现第二个属性，Raft将在`AppendEntries`中附加如下规则：

为了使跟随者的日志与自己的日志保持一致，领导者必须找到两个日志一致的最新日志条目，删除跟随者日志中该条目之后的任何条目，并将这之后的所有条目发送给跟随者。

领导者将为每个跟随者维护一个`NextIndex`，这将是领导者发送给追随者的下一个日志条目的索引。当一个领导者刚被任命时，其将初始化每个`NextIndex`为其日志中最后一个日志条目index的值加一。当每次发送`AppendEntries`时，跟随者将检查其`NextIndex-1`处的日志是否与领导者该处的日志一致，如不一致则向领导者返回**一致性检查失败**的信息。当领导者观察到一致性检查失败时，其将减小该跟随者的`NextIndex`。该过程将不断重复直到跟随者满足一致性检查。在实际系统中，`NextIndex`减小的过程可以被优化，详见下文。

领导者会定期检查，哪些日志条目已经被复制到了大多数服务器中，并将`CommitIndex`更新为该种日志条目中index数最高的数值，以提交该日志条目及其之前的所有条目。同时，领导者会在`AppendEntries`中发送`CommitIndex`，以向跟随者通知哪些日志条目被提交。

### 安全性保证

在Raft中，为了保证共识算法的安全性，需要为算法添加额外的限制。

#### 领导者限制

在共识算法中，领导者必须拥有所有已**提交**的日志条目，Raft中使用投票过程阻止服务器赢得选举，除非它的日志中包含所有已提交的条目。由于选举过程中候选者必须获得集群中的大多数选票才能当选，且已提交的日志项目存在于大部分服务器中，因此每个已提交的条目将存在于至少一个服务器中。这意味着，一次具有大多数服务器参与的选举中，一定存在一个包含所有已提交条目的服务器，Raft将保证该种服务器被最后选举为领导者。

在Raft中，利用**日志匹配属性**来保证上述规则。由于该属性，如果一个服务器日志的最后一个日志条目相较于另一个服务器之日的最后一个日志条目更新（term数更大、term数相同且index数更大），则前服务器的日志中拥有后服务器的所有日志条目。因此，在选举过程中，跟随者需要其与向其发送`RequestVote`的候选者比较日志新旧，如果候选者的日志比跟随着更旧，则拒绝投票。

#### 提交限制

在Raft中，领导者仅通过检查当前term数的日志条目来更新`CommitIndex`，该限制被称为**提交限制**。该限制的提出基于下面一个隐患：

![Lab2_4](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_4.png)

在上图中，阐述了若不设定提交限制会发生的隐患：

（a）S1在term2时复制日志条目<2,2>（使用<term,index>数代表日志条目），但在尚未复制完时崩溃；

（b）S5担任了term3的领导者，客户端向其发送日志条目<3,2>，但在复制前崩溃；

（c）S1继续担任term4的领导者，并继续复制日志条目<2,2>。此时，若无提交限制，则日志条目<2,2>则被提交，这导致了一个隐患；

（d）隐患在于，若在此时S1崩溃、S5担任领导者（此时满足领导者限制），则S5将会将其他服务器中已被提交的日志条目<2,2>覆盖，该行为违反了**状态机安全属性**；

因此，在Raft中需要提交限制来限制该行为，在该限制下，S1不会在(c)中提交日志条目<2,2>，因此S5在(d)中将其覆盖是安全的；如果S1在(c)中不崩溃，而继续在term4中复制日志条目<4,3>，则提交<4,3>是被允许的。此时由于大多数服务器（S1,S2,S3）的日志是最新的，因此即使S1崩溃，下一个领导者也只会在日志最新的服务器中选取，因此被提交的日志条目不会被覆盖。

### 日志压缩

为了阻止日志的无限制增长，必须在日志条目达到一定数量时对日志进行压缩：

![Lab2_5](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_5.png)	

在Raft中，使用快照功能实现日志压缩。在服务器运行过程中（不仅限于领导者），会定期检查当前日志的长度，如果达到了一定阈值，则会将以应用到该服务器状态机的日志对状态机的影响压缩成快照（snapshot）的形式存放在服务器中，同时删除日志中所有以应用到状态机的日志条目。如上图所示，x<-3、y<-1、y<-9、x<-2、x<-0的日志条目可以被压缩为<x<-0、y<-9>的快照。

但是，快照的功能引出了一个日志复制过程中的问题。例如，在上图中，index数小于5的日志条目均被删除，此时若存在服务器需求日志条目<1,3>，该服务器则必须通过另一种形式发送被删除的该条目。在这里，Raft通过发射快照的形式解决该问题，即如果其他服务器需求领导者中已被删除的日志条目，则领导者会通过`InstallSnapshot`RPC向其发送快照代替日志条目的发送。

为了保证删除日志条目前后日志的一致性，在这里服务器必须记录被删除的最高日志条目的term和index数，被称为`rf.LastIncludedTerm`和`rf.LastIncludedIndex`。

## 具体代码实现

在Raft论文中的图2及图13中描述了Raft具体编写的大体细节，在下文中将按照论文中的变量名和函数名进行编写，不再重复介绍论文中的变量名和函数名。

### 功能函数

在这里，为了提高代码的可读性，将Raft中的一些频繁使用的功能设计成了函数的形式，并存放于`util.go`中：

```go
func (rf *Raft) GetLastEntry() LogEntry {
	if len(rf.Log) == 0 {
		return LogEntry{Term: rf.LastIncludedTerm, Index: rf.LastIncludedIndex}
	}
	return rf.Log[len(rf.Log)-1]
}
func (rf *Raft) GetFirstEntry() LogEntry {
	if len(rf.Log) == 0 {
		return LogEntry{Term: rf.LastIncludedTerm, Index: rf.LastIncludedIndex}
	}
	return rf.Log[0]
}
func GetElectionTimeout() int {
	rand.Seed(time.Now().UnixNano())
	return ELECTIONTIMEOUTBASE + int(rand.Int31n(ELECTIONTIMEOUTRANGE))
}
func (rf *Raft) GetLogIndex(index int) LogEntry {
	if index == 0 {
		return LogEntry{Term: -1, Index: 0}
	} else if index == rf.LastIncludedIndex {
		return LogEntry{Term: rf.LastIncludedTerm, Index: rf.LastIncludedIndex}
	} else {
		return rf.Log[index-rf.LastIncludedIndex-1]
	}
}
func Min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
func Max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
```

`GetLastEntry()`：获取服务器日志中最后一个日志条目，如日志为空则返回<`rf.LastIncludedTerm`,`rf.LastIncludedIndex`>

`GetFirstEntry()`：获取服务器日志中第一个日志条目，如日志为空则返回<`rf.LastIncludedTerm`,`rf.LastIncludedIndex`>

`GetElectionTimeout()`：获取一个随机的**选举超时时间**，该时间由基础值`ELECTIONTIMEOUTBASE`与随机范围`ELECTIONTIMEOUTRANGE`组成

`GetLogIndex()`：根据index数获取日志中实际的日志条目，考虑了日志压缩导致的日志条目删除以及边界条件（index = 0或index = `LastIncludedIndex`）

`Min()、Max()`：更大更小值函数

### 基本数据结构

```go
type LogEntry struct {
	Command interface{}
	Term    int
	Index   int
}
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()
	applych   chan ApplyMsg
	cond      *sync.Cond
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
```

在这里，`LogEntry`为日志条目数据结构，包含命令、index、term；`Raft`为服务器的元数据数据结构，除了论文中提过的变量以外，使用`mu`互斥锁保护数据结构、`cond`条件变量用于唤醒可能睡眠的候选者、`persister`代替磁盘进行可容错存储、`peers`代表集群中的所有服务器、`me`表示自身在`peers`中的索引、`BroadcastTime`为领导者发送`AppendEntries`的间隔时间、`ElectionTimeout`为选举超时时间。

```go
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.mu.Lock()
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applych = applyCh
	rf.cond = sync.NewCond(&rf.mu)
	DPrintf("[%d] is Making , len(peers) = %d\n", me, len(peers))
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
```

在`Make`中，初始化Raft服务器的元数据，调用`readPersist`读入磁盘中可能存在的一些需要可持久化的变量（下文中描述），并开始线程运行服务器主函数`ticker()`。

```go
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	_, isLeader := rf.GetState()
	if !isLeader || rf.killed() {
		DPrintf("[%d] Start() Fail isleader = %t, isKilled = %t", rf.me, isLeader, rf.killed())
		return -1, -1, false
	}
	// Your code here (2B).
	rf.mu.Lock()
	logEntry := LogEntry{Command: command, Term: rf.CurrentTerm, Index: rf.GetLastEntry().Index + 1}
	rf.Log = append(rf.Log, logEntry)
	rf.persist()
	rf.mu.Unlock()
	DPrintf("[%d] Start() ,index : %d,term : %d,command : %d", rf.me, logEntry.Index, logEntry.Term, command)
	return logEntry.Index, logEntry.Term, isLeader
}
```

在`Start`中，客户端向Raft服务器发送命令，创建一个日志条目并插入本地日志即可。

### 服务器主函数ticker

在服务器运行过程中，将无限期的运行ticker函数以执行服务器在Raft集群中的所有任务，直到其被杀死：

```go
func (rf *Raft) ticker() {
	me := rf.me
	for rf.killed() == false {
		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		rf.mu.Lock()
		rf.UpdateApplied()
		State := rf.State
		if State == LEADER {
			rf.mu.Unlock()
			rf.TrySendEntries(false)
			rf.UpdateCommitIndex()
			time.Sleep(time.Duration(rf.BroadcastTime) * time.Millisecond)
		} else if State == FOLLOWER {
			DPrintf("[%d] ElectionTimeout = %d", me, rf.ElectionTimeout)
			if rf.ElectionTimeout < rf.BroadcastTime {
				rf.State = CANDIDATE
				DPrintf("[%d] ElectionTimeout,convert to CANDIDATE\n", me)
				rf.mu.Unlock()
				continue
			}
			rf.ElectionTimeout -= rf.BroadcastTime
			rf.mu.Unlock()
			time.Sleep(time.Duration(rf.BroadcastTime) * time.Millisecond)
		} else if State == CANDIDATE {
			rf.DoElection()
		}
	}
}
```

在这里，服务器通过`rf.killed()`判断其是否被杀死，如未被杀死，则继续循环。在一次循环中，服务器将完成如下工作：

1. 调用`rf.UpdateApplied()`更新`LastApplied`，如成功更新`LastApplied`则将被更新的日志条目中的命令发送至状态机；
2. 检查自身的状态，并分情况执行不同的任务。如果状态为领导者，则调用`TrySendEntries(initialize bool)`尝试发送日志条目，在该函数中可能执行心脏RPC、日志复制或快照复制，其参数代表此次函数是否为该服务器称为领导者后的第一次调用；调用`UpdateCommitIndex()`尝试更新`CommitIndex`；并在心跳间隔时间`BroadcastTime`内睡眠，以实现心跳间隔。
3. 如状态为跟随者，则检查当前`ElectionTimeout`是否即将归零，判断标准为将`ElectionTimeout`与`BroadcastTime`比较。如未即将归零，则在心跳间隔时间`BroadcastTime`内睡眠，并递减`ElectionTimeout`；如即将归零，则将状态转化为候选者。
4. 如状态为候选者，则调用`DoElection()`开始选举。

```go
func (rf *Raft) UpdateApplied() {
	for rf.LastApplied < rf.CommitIndex {
		if rf.LastApplied >= rf.LastIncludedIndex {
			msg := ApplyMsg{CommandValid: true, Command: rf.GetLogIndex(rf.LastApplied + 1).Command, CommandIndex: rf.LastApplied + 1}
			rf.mu.Unlock()
			rf.applych <- msg
			DPrintf("[%d] apply msg %d success", rf.me, rf.LastApplied+1)
			rf.mu.Lock()
		}
		rf.LastApplied++
	}
}
```

在`UpdateApplied()`中，服务器检查`LastApplied`的值是否小于`CommitIndex`，若大于则以`ApplyMsg`的形式发送`CommitIndex`至`LastApplied+1`的日志条目到`rf.applych`以应用命令到状态机（为了防止发送已被日志压缩删除的条目，需要特殊判断），注意为了防止死锁，在发送信息到channel的过程中不能保持锁。

```go
func (rf *Raft) UpdateCommitIndex() {
	rf.mu.Lock()
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
	DPrintf("[%d] Update CommitIndex, term = %d,NextIndex is %v,MatchIndex is %v,CommitIndex is %d", rf.me, rf.CurrentTerm, rf.NextIndex, rf.MatchIndex, rf.CommitIndex)
	rf.mu.Unlock()
}
```

寻找在当前term下`MatchIndex[i]`大于服务器数量一半的最高索引N，若N大于`CommitIndex`则更新本地`CommitIndex`。

### 持久化函数

如论文所述，Raft必须按时保存一些需要被持久化的变量于容错介质，具体需要被持久化的变量为`CurrentTerm`、`VotedFor`、`Log`、`LastIncludedIndex`、`LastIncludedTerm`。

```go
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
	DPrintf("[%d] call Persist(),rf.CurrentTerm = %d,rf.VotedFor = %d,rf.Log len = %d,lastIncludedIndex = %d", rf.me, rf.CurrentTerm, rf.VotedFor, len(rf.Log), rf.LastIncludedIndex)
}
```

```go
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
		DPrintf("[%d] call readPersist(),rf.CurrentTerm = %d,rf.VotedFor = %d,rf.Log len = %d,lastIncludedIndex = %d", rf.me, rf.CurrentTerm, rf.VotedFor, len(rf.Log), lastIncludedIndex)
	}
}
```

在这里，使用`labgob.NewEncoder`和`labgob.NewDecoder`将上述变量写入和写出`persister`。在下文中，将在任意改变上述变量的地方调用`persist()`。

### 选举功能函数

```go
func (rf *Raft) DoElection() {
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
	DPrintf("[%d] Start Election,term = %d\n", rf.me, term)
	rf.mu.Unlock()
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go func(server int) {
				args := RequestVoteArgs{Term: term, CandidateId: rf.me, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm}
				reply := RequestVoteReply{}
				DPrintf("[%d] send RequestVote to server %d\n", rf.me, server)
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
			DPrintf("[%d] is voted as LEADER(term : %d)\n", rf.me, rf.CurrentTerm)
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
	DPrintf("[%d] Do election finished", rf.me)

}
```

在`DoElection()`中，候选者完成所有的选举操作，其由以下流程组成：

1. 进行一些准备操作，包括增加当前term、并将`VotedFor`设置为自身、重置超时时间、以及为RPC调用设置参数；

2. 向其他服务器并发的发送`RequestVote`，注意在发送RPC的时候服务器不得持有锁（导致死锁）。若RPC调用失败则直接终止对应的线程；若RPC调用成功，则查看选票是否通过，如通过则递增`voteGranted`。如果跟随者的term数大于本服务器的term数，则更新term数，并立即转化为跟随者。最后，无论调用是否成功，RPC返回时都应调用`rf.cond.Broadcast()`唤醒主线程。

3. 在主线程中，候选者运行超时唤醒线程，其在经过`electionTimeout`后将`timeout`原子地置为1，并唤醒主线程，以提醒超时；

4. 随后，主线程判断选举是否结束（获得大多数选票、term数变化、身份变化、超时），若尚未结束则调用`rf.cond.Wait()`睡眠；若选举结束或被唤醒，则检查是否获得了大多数选票。如获得了大多数选票，则将状态置为领导者，并初始化`CommitIndex`、`MatchIndex`、`NextIndex`。随后，调用`TrySendEntries`发送心跳信息或复制日志。


```go
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}
```

在这里，`RequestVote`的参数和结果结构体与论文所写的相同。

```go
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	//DPrintf("[%d] Get RequestVote from %d", rf.me, args.CandidateId)
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
		DPrintf("[%d %d] VoteFor %d(term : %d)\n", rf.me, rf.State, args.CandidateId, rf.CurrentTerm)
	}
}
```

在`RequestVote`中，本地服务器首先判断其与候选者term数的差异，若本地服务器term数大于候选者term数，则拒绝投票并返回；如本地服务器term数小于候选者term数，则将当前状态转化为追随者，重置`VotedFor`并更新term数。然后，服务器检查选票是否存在（`VotedFor`是否为1），并检查**领导者限制**，如均检查通过则投票给此候选者，并填充`rf.VotedFor`。

值得注意的是，在接受到`RequestVote`时，尽在选票通过时重置本地超时时间，以防止不符合约束的候选者一直阻塞满足约束的潜在候选者。

### 日志复制及心跳

```go
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
```

在候选者选举胜利及领导者每心跳间隔时，服务器将调用`TrySendEntries`进行日志复制或心跳RPC。在函数中，服务器检查其他服务器的当前状态，并决定具体发送的RPC类型：

1. 若本地服务器日志中最后一个日志条目的index大于某服务器的`nextIndex`，或当前函数为候选者选举胜利被调用的，且当前日志中存在需要被发送的日志条目（`firstLogIndex <= nextIndex`），则执行`rf.SendEntries(i)`进行日志复制；
2. 若当前日志中不存在需要被发送的日志条目（`firstLogIndex <= nextIndex`），则执行`rf.SendSnapshot(i)`进行快照复制；
3. 否则，执行`rf.SendHeartBeat(i)`发送心跳RPC。

```go
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
		DPrintf("[%d] send Entries to server %d,prevLogIndex = %d,prevLogTerm = %d,LastIncludeIndex = %d\n", rf.me,
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
			DPrintf("[%d] rf.NextIndex[%d] update to %d", rf.me, server, rf.NextIndex[server])
			done = false
		} else {
			rf.NextIndex[server] = prevLogIndex + len(entries) + 1
			rf.MatchIndex[server] = prevLogIndex + len(entries)
			DPrintf("[%d] AppendEntries success,NextIndex is %v,MatchIndex is %v", rf.me, rf.NextIndex, rf.MatchIndex)
		}
		rf.mu.Unlock()
	}
}
```

`SendEntries`实现了日志复制操作，在函数中，循环向目标服务器发送`AppendEntries`直到日志发送成功或出错，每次循环包括如下操作：

1. 判断当前状态是否仍为领导者，以及当前日志是否有需要被发送的日志条目，如不满足则直接返回。（double-check，为了防止在循环释放锁的过程中上述参数被改变）；
2. 填充RPC参数，注意在这里为了节省开销，在`args.entries`中仅存放`NextIndex`即之后的日志条目。发送`AppendEntries`，如RPC发送失败则直接返回；
3. 如接受方的term数大于领导者的term数，则更新term数、转换为跟随者并重置超时时间；
4. 如接收方**一致性检查失败**，则减少当前的`NextIndex`，在这里采取了实验指导书中的优化方式：

![Lab2_6](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/noteFigures/Lab2_5.png)

​	接受方填充了`XTerm` - 接收方冲突条目的term数、`XIndex` - 接收方term数为`XTerm`的条目中的最低索引、`XLen` - 接收方日志长度（实际上为接收方最后一个日志条目的index）。当`XLen`小于`prevLogIndex`，符合上图中的Case3，在这里将`NextIndex`置为max（XLen，1），以防止该值等于0（使得下次循环中`prevLogIndex`为-1）；对于其他Case，按照上图完成即可，同样需要注意防止`NextIndex`等于0。

5.如接收方通过了本次`AppendEntries`，则直接返回即可。

```go
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
```

`AppendEntries`的参数和返回值与论文中相同，在返回值中加上了用于优化重传的`XTerm`等元素。

```go
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {

	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("[%d %d] get AppendEntries from %d, LastIncludeIndex = %d,logs = %v,currterm = %d(PrevLogIndex:%d,PrevLogTerm:%d,Leaderterm:%d)\n",
		rf.me, rf.State, args.LeaderId, rf.LastIncludedIndex, rf.Log, rf.CurrentTerm, args.PrevLogIndex, args.PrevLogTerm, args.Term)
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
		DPrintf("[%d %d] AppendEntries fail because of consistence, XLen = %d, XTerm = %d, XIndex = %d", rf.me, rf.State, reply.XLen, reply.XTerm, reply.XIndex)
		reply.Success = false
		return
	}
	if len(args.Entries) != 0 {
		var log []LogEntry
		for i := rf.LastIncludedIndex + 1; i <= args.PrevLogIndex; i++ {
			log = append(log, rf.GetLogIndex(i))
		}
		for i := range args.Entries {
			log = append(log, args.Entries[i])
		}
		rf.Log = log
		rf.persist()
		DPrintf("[%d %d] Append new log", rf.me, rf.State)
	}
	if args.LeaderCommit > rf.CommitIndex {
		rf.CommitIndex = Min(args.LeaderCommit, rf.GetLastEntry().Index)
		DPrintf("[%d %d] CommitIndex update to %d\n", rf.me, rf.State, rf.CommitIndex)
	}
}
```

当服务器收到`AppendEntries`时，其根据以下顺序完成任务：

1. 立即重置自身的选举超时时间，以避免在有其他有效领导者的情况下开始选举；
2. 若发送方的term小于接收方的term，或发送方的`prevLogIndex`处的日志条目已经被接收方在日志压缩中删除，则返回`reply.success`为假。注意，在后者情况下，`XLen`被默认初始化为0，因此发送方的`NextIndex`将被设定为Max(0,1)即1，将在`SendEntries` 中返回，并在下次`TrySendEntries`中发送快照。
3. 若接收方的term小于发送方的term，或接收方为候选者，则转化为跟随者，并调用`rf.cond.Broadcast()`唤醒选举函数`DoElection`主线程。
4. 若接收方**一致性检查失败**，则填充`XTerm`、`XIndex`、`XLen`并返回`reply.success`为假。
5. 若一致性检查通过，则在RPC参数中日志条目不为0的情况下更改本地日志，即删除本地`prevLogIndex`以后的所有日志条目，并把`arg.Entries`中的所有条目附加在本地日志。
6. 尝试更新本地`CommitIndex`。

```go
func (rf *Raft) SendHeartBeat(server int) {
	rf.mu.Lock()
	DPrintf("[%d] send heartBeat to server %d\n", rf.me, server)
	if rf.State != LEADER {
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
	rf.sendAppendEntries(server, &args, &reply)
	rf.mu.Lock()
	if reply.Term > rf.CurrentTerm {
		rf.CurrentTerm = reply.Term
		rf.State = FOLLOWER
		rf.ElectionTimeout = GetElectionTimeout()
		rf.VotedFor = -1
		rf.persist()
	}
	rf.mu.Unlock()
}
```

`SendHeartBeat`执行心跳RPC的发送，在函数中填充`AppendEntries`中除`Entries`外的所有元素，并发送`AppendEntries`请求。后续的处理仅为检查接受方的term是否大于本地term，并在上述情况发生时转化为跟随者并重置选举超时时间。

### 日志压缩

```go
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	rf.mu.Lock()
	DPrintf("[%d] CALL Snapshot index = %d", rf.me, index)
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
```

在`Snapshot`执行具体的日志压缩工作，方法即为删除`index`及之前的所有日志条目，并将`LastIncludedIndex`更新为index。注意在`SnapShot`中要使用`SaveStateAndSnapshot`持久化当前状态和快照。

```go
func (rf *Raft) SendSnapshot(server int) {
	rf.mu.Lock()
	term := rf.CurrentTerm
	leaderId := rf.me
	lastIncludedIndex := rf.LastIncludedIndex
	lastIncludedTerm := rf.LastIncludedTerm
	snapshot := rf.persister.snapshot
	args := InstallSnapshotArgs{Term: term, LeaderId: leaderId, LastIncludedIndex: lastIncludedIndex, LastIncludedTerm: lastIncludedTerm, Snapshot: snapshot}
	reply := InstallSnapshotReply{}
	DPrintf("[%d] send SnapShot to server %d,LastIncludeIndex = %d\n", rf.me,
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
```

如上文所述，如跟随者所需的日志条目已经在之前的日志压缩中被删除，则需要向跟随者发送快照。`SendSnapshot`执行具体的发送快照功能，其填充RPC参数，并发送`InstallSnapshot`RPC。若RPC发送成功，则检查接收方term数，如大于本地term则转化为跟随者。最后，因为`LastIncludedIndex`及之前的所有日志条目已经通过快照的形式被发送，所以需要将`NextIndex`设置为`LastIncludedIndex+1`，并将`MatchIndex`设置为`rf.LastIncludedIndex`。

```go
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
```

`InstallSnapshot`与论文中的相同，但不用将RPC分片。

```go
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	reply.Term = rf.CurrentTerm
	rf.ElectionTimeout = GetElectionTimeout()
	if args.Term < rf.CurrentTerm || args.LastIncludedIndex <= rf.LastIncludedIndex {
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
			rf.mu.Unlock()
			rf.applych <- ApplyMsg{SnapshotValid: true, Snapshot: args.Snapshot, SnapshotTerm: args.LastIncludedTerm, SnapshotIndex: args.LastIncludedIndex}
			return
		}
	}
	rf.Log = make([]LogEntry, 0)
	rf.persist()
	DPrintf("[%d] InstallSnapShot success,LastIncludeIndex = %d\n", rf.me, rf.LastIncludedIndex)
	rf.mu.Unlock()
	rf.applych <- ApplyMsg{SnapshotValid: true, Snapshot: args.Snapshot, SnapshotTerm: args.LastIncludedTerm, SnapshotIndex: args.LastIncludedIndex}
	DPrintf("[%d] apply snapshot,LastIncludeIndex = %d\n", rf.me, rf.LastIncludedIndex)
}
```

当跟随者接收到`InstallSnapshot`时，其更新自身的快照以及日志，其由以下流程组成：

1. 若发送方的term数小于接收方的term数，或发送方的`LastIncludedIndex`不大于接收方的`LastIncludedIndex`，则直接返回；
2. 将RPC参数中的快照通过`SaveStateAndSnapshot`持久化到本地；
3. 若`LastIncludedIndex`小于本地日志中最后一个日志条目的index，则删除`LastIncludedIndex`及之前的所有日志条目，并将快照以`ApplyMsg`的形式发送到`rf.applych`以应用到状态机。需要注意在发送到channel的过程中，不能持有锁；
4. 若`LastIncludedIndex`大于等于本地日志中最后一个日志条目的index，则删除所有日志条目，并将快照以`ApplyMsg`的形式发送到`rf.applych`以应用到状态机；

## 实验结果

上述代码可以在200次`go test`中保证不出错，可以证明其具有较好的可靠性。

```shell
Test (2A): initial election ...
  ... Passed --   4.0  3   64   18110    0
Test (2A): election after network failure ...
  ... Passed --   7.1  3  152   32758    0
Test (2A): multiple elections ...
  ... Passed --  10.0  7  963  203903    0
Test (2B): basic agreement ...
  ... Passed --   2.3  3   24    6752    3
Test (2B): RPC byte count ...
  ... Passed --   4.6  3   72  121196   11
Test (2B): agreement after follower reconnects ...
  ... Passed --   7.5  3  117   31781    8
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   5.0  5  188   46654    3
Test (2B): concurrent Start()s ...
  ... Passed --   1.4  3   12    3384    6
Test (2B): rejoin of partitioned leader ...
  ... Passed --   7.1  3  169   40141    4
Test (2B): leader backs up quickly over incorrect follower logs ...
  ... Passed --  39.4  5 2905 2577161  102
Test (2B): RPC counts aren't too high ...
  ... Passed --   3.4  3   42   12322   12
Test (2C): basic persistence ...
  ... Passed --   8.7  3  132   33130    6
Test (2C): more persistence ...
  ... Passed --  26.5  5 1230  286502   16
Test (2C): partitioned leader and one follower crash, leader restarts ...
  ... Passed --   5.1  3   63   16379    4
Test (2C): Figure 8 ...
  ... Passed --  33.0  5  418   94161   11
Test (2C): unreliable agreement ...
  ... Passed --  11.7  5  424  137045  246
Test (2C): Figure 8 (unreliable) ...
  ... Passed --  39.8  5 3152 11039598  172
Test (2C): churn ...
  ... Passed --  16.5  5  315  106266   50
Test (2C): unreliable churn ...
  ... Passed --  16.5  5  608  288568  121
Test (2D): snapshots basic ...
  ... Passed --  11.1  3  202   68384  202
Test (2D): install snapshots (disconnect) ...
  ... Passed --  69.8  3 1560  579176  342
Test (2D): install snapshots (disconnect+unreliable) ...
  ... Passed --  75.9  3 1682  606675  352
Test (2D): install snapshots (crash) ...
  ... Passed --  57.0  3  898  444416  365
Test (2D): install snapshots (unreliable+crash) ...
  ... Passed --  60.9  3  965  404233  302
Test (2D): crash and restart all servers ...
  ... Passed --  26.9  3  442  129816   67
PASS
ok  	6.824/raft	551.116s
```

