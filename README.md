# MIT6.824 Lab

MIT6.824相关实验的源码及解答笔记，在本实验中需要完成...（更新中）

## 课程实验笔记

### Lab 1 : MapReduce [note1](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab1.md)

在本实验中，需要构建一个简单的分布式系统MapReduce，用户需要根据任务的需求，将任务拆分为Map和Reduce两个部分，并编写对应的函数用于使用系统接口。通过这种方式，用户可以在忽略并行化、容错等细节的条件下完成任务的分布式并发计算。

### Lab 2 : Raft [note2](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab2.md)

在本实验中，需要实现Raft机制，其为一种复制状态机协议，用于实现分布式系统间的共识。Raft将客户端请求组织成一个序列，称为日志，Raft保证所有副本服务器看到相同的日志，并按日志顺序执行客户端请求，并将它们应用到服务器的本地副本，因此，所有副本服务器将拥有相同的服务状态。
