package mr

import (
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type Coordinator struct {
	// Your definitions here.
	MapTask      []Task
	ReduceTask   []Task
	Contents     []string
	FileName     []string
	MapRemain    int
	ReduceRemain int
	Cv           *sync.Cond
}

// Your code here -- RPC handlers for the worker to call.

//
// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
//
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}
func (c *Coordinator) AskMapTask(args *AskMapArgs, reply *AskMapReply) error {
	// find a map task first, maybe IDLE or timeout
	currTime := time.Now()
	c.Cv.L.Lock()
	defer c.Cv.L.Unlock()
	if c.MapRemain == 0 {
		reply.TaskId = -1
		reply.Over = true
		return nil
	}
	for i, task := range c.MapTask {
		dur := currTime.Sub(c.MapTask[i].StartTime)
		if task.State == IDLE || (task.State == IN_PROGRESS && dur.Seconds() > 10.0) {
			c.MapTask[i].State = IN_PROGRESS
			c.MapTask[i].WorkerMachine = Machine{args.WorkerMachine.Path}
			c.MapTask[i].StartTime = currTime
			reply.Content = c.Contents[i]
			reply.Filename = c.FileName[i]
			reply.TaskId = i
			reply.NReduce = len(c.ReduceTask)
			reply.Over = false
			return nil
		}
	}
	reply.Over = false
	reply.TaskId = -1
	return nil
}

func (c *Coordinator) MapOver(args *TaskOverArgs, reply *TaskOverReply) error {
	c.Cv.L.Lock()
	defer c.Cv.L.Unlock()
	i := args.TaskId
	c.MapTask[i].State = COMPLETE
	c.MapRemain--
	if c.MapRemain == 0 {
		c.Cv.Broadcast()
	}
	//fmt.Printf("map task %d over\n", i)
	return nil
}

func (c *Coordinator) AskReduceTask(args *AskReduceArgs, reply *AskReduceReply) error {
	c.Cv.L.Lock()
	defer c.Cv.L.Unlock()
	if c.MapRemain != 0 {
		c.Cv.Wait()
	}
	if c.ReduceRemain == 0 {
		reply.TaskId = -1
		reply.Over = true
		return nil
	}
	currTime := time.Now()
	for i, task := range c.ReduceTask {
		dur := currTime.Sub(c.ReduceTask[i].StartTime)
		if task.State == IDLE || (task.State == IN_PROGRESS && dur.Seconds() > 10.0) {
			c.ReduceTask[i].State = IN_PROGRESS
			c.ReduceTask[i].WorkerMachine = Machine{args.WorkerMachine.Path}
			c.ReduceTask[i].StartTime = currTime
			for _, task := range c.MapTask {
				reply.IntermediateMachine = append(reply.IntermediateMachine, task.WorkerMachine)
			}
			reply.TaskId = i
			reply.Over = false
			return nil
		}
	}
	reply.TaskId = -1
	reply.Over = false
	return nil
}

func (c *Coordinator) ReduceOver(args *TaskOverArgs, reply *TaskOverReply) error {
	c.Cv.L.Lock()
	defer c.Cv.L.Unlock()
	i := args.TaskId
	c.ReduceTask[i].State = COMPLETE
	c.ReduceRemain--
	//fmt.Printf("reduce task %d over\n", i)
	return nil
}

//
// start a thread that listens for RPCs from worker.go
//
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
//
func (c *Coordinator) Done() bool {

	// Your code here.
	c.Cv.L.Lock()
	defer c.Cv.L.Unlock()
	return c.ReduceRemain == 0
}

//
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.
	nMap := len(files)
	c.MapTask = make([]Task, nMap)
	c.ReduceTask = make([]Task, nReduce)
	c.Contents = make([]string, nMap)
	c.FileName = make([]string, nMap)
	c.MapRemain = nMap
	c.ReduceRemain = nReduce
	c.Cv = sync.NewCond(&sync.Mutex{})
	for i, filename := range os.Args[1:] {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
		}
		content, err := ioutil.ReadAll(file)
		c.Contents[i] = string(content)
		c.FileName[i] = filename
		if err != nil {
			log.Fatalf("cannot read %v", filename)
		}
		file.Close()
	}

	c.server()
	return &c
}
