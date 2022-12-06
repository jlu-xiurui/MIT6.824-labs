# MIT6.824 Lab

MIT6.824相关实验的源码及解答笔记，在本实验中需要完成...（更新中）

## 课程实验笔记

### Lab 1 : MapReduce [note1](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab1.md)

在本实验中，需要构建一个简单的分布式系统MapReduce，用户需要根据任务的需求，将任务拆分为Map和Reduce两个部分，并编写对应的函数用于使用系统接口。通过这种方式，用户可以在忽略并行化、容错等细节的条件下完成任务的分布式并发计算。

### Lab 2 : Raft [note2](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab2.md)

在本实验中，需要实现Raft机制，其为一种复制状态机协议，用于实现分布式系统间的共识。Raft将客户端请求组织成一个序列，称为日志，Raft保证所有副本服务器看到相同的日志，并按日志顺序执行客户端请求，并将它们应用到服务器的本地副本，因此，所有副本服务器将拥有相同的服务状态。

### Lab 3 : Fault-tolerant Key/Value Service [note3](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab3.md)

在本实验中，需要实现一个分布式的KV存储库。在这里，分布式为KV存储库提供了容错的能力，即只要分布式集群中大多数KV存储库服务器可以工作，集群就可以为客户端提供KV存储库服务。其中，分布式KV存储库使用Raft库，使得客户端的操作将以日志的形式在服务器间进行复制，以保证系统的强一致性。本实验中，客户端所连续发送的操作需要在平均三分之一Raft心跳间隔内被提交，这使得需要对Lab2中的Raft实现进行更改。
