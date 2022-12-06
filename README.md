# MIT6.824 Lab

MIT6.824相关实验的源码及解答笔记，在本课程中可以阅读若干分布式相关论文，并在讲座中对其进行讲解。通过论文，了解实际工程中的分布式系统的实现思路、细节，并对不同论文中所实现的系统的设计理念、目标任务进行对比。 

并且，在课程实验中需要使用Golang语言完成一个分片的分布式KV存储系统，通过分布式方法，存储系统可以在保证“线性一致性”的前提下获得容错性以及更高的并行性。通过课程实验，可以获取一定的分布式编程经验，通过实践了解分布式的相关理论，并且可以很好的锻炼Golang语言的编写能力。

## 课程实验笔记

### Lab 1 : MapReduce [note1](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab1.md)

在本实验中，需要构建一个简单的分布式系统MapReduce，用户需要根据任务的需求，将任务拆分为Map和Reduce两个部分，并编写对应的函数用于使用系统接口。通过这种方式，用户可以在忽略并行化、容错等细节的条件下完成任务的分布式并发计算。

### Lab 2 : Raft [note2](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab2.md)

在本实验中，需要实现Raft机制，其为一种复制状态机协议，用于实现分布式系统间的共识。Raft将客户端请求组织成一个序列，称为日志，Raft保证所有副本服务器看到相同的日志，并按日志顺序执行客户端请求，并将它们应用到服务器的本地副本，因此，所有副本服务器将拥有相同的服务状态。

### Lab 3 : Fault-tolerant Key/Value Service [note3](https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab3.md)

在本实验中，需要实现一个分布式的KV存储库。在这里，分布式为KV存储库提供了容错的能力，即只要分布式集群中大多数KV存储库服务器可以工作，集群就可以为客户端提供KV存储库服务。其中，分布式KV存储库使用Raft库，使得客户端的操作将以日志的形式在服务器间进行复制，以保证系统的强一致性。本实验中，客户端所连续发送的操作需要在平均三分之一Raft心跳间隔内被提交，这使得需要对Lab2中的Raft实现进行更改。

### Lab 4 :  Sharded Key/Value Service [note4]https://github.com/jlu-xiurui/MIT6.824-labs/blob/master/notes/MIT6.824%20Lab4..md)

在本实验中，需要实现分片KV存储系统，其中系统的键将被映射至对应的分片中，各分片将被均匀的分配到若干副本组中进行服务，即每个副本组仅需处理其对应的分片的操作，这使得不同组之间可并行操作，以提升系统的性能。

在这里，分片KV存储系统可以被分为两个部分，其一为**“分片控制器”，其决定哪个副本组应当为哪些分片服务，以及副本组所对应的服务器集群，该信息也被称作为配置**；其二为**“副本组”**，是由一组KV存储服务器所组成的Raft集群，其通过访问分片控制器来获取当前的配置信息，并为对应的分片提供服务，当配置更改时，副本组应当将其拥有的分片进行转移。
