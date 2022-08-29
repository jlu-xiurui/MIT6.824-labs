package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/rpc"
	"os"
)

//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

//
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

//
// main/mrworker.go calls this function.
//

// try to do the map task, return the result of map task : false / task id
func DoMapTask(mapf func(string, string) []KeyValue) int {
	// call RPC to get content for Map
	path, err := os.Getwd()
	if err != nil {
		fmt.Printf("Getwd failed!\n")
		return -1
	}
	args := AskMapArgs{Machine{path}}
	reply := AskMapReply{}
	ok := call("Coordinator.AskMapTask", &args, &reply)
	if ok {
		fmt.Printf("Call AskMapTask failed!\n")
		return -1
	}
	// All Map task is busy or over
	if reply.TaskId == 0 {
		return 0
	}
	// Do the user Map
	taskId := reply.TaskId
	nReduce := reply.nReduce
	kva := mapf(reply.Filename, string(reply.Content))
	intermediate := make([][]KeyValue, nReduce)
	for _, kv := range kva {
		i := ihash(kv.Key) % nReduce
		intermediate[i] = append(intermediate[i], kv)
	}
	for i := 0; i < nReduce; i++ {
		filename := fmt.Sprintf("mr-%d%d", taskId, i)
		file, _ := os.Create(filename)
		enc := json.NewEncoder(file)
		for _, kv := range intermediate[i] {
			err := enc.Encode(&kv)
			if err != nil {
				fmt.Printf("Encode failed!\n")
				return -1
			}
		}
		file.Close()
	}
	return taskId
}

func DoMapTask(reduce func(string, string)) int {
}
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.

	// uncomment to send the Example RPC to the coordinator.
	// CallExample()
	for {
		// try DoMapTask, if OK orr cause err, then continue and try another map task
		if ret := DoMapTask(mapf); ret > 0 {
			args := MapOverArgs{ret}
			reply := MapOverReply{}
			for {
				if ok := call("Coordinator.MapOver", &args, &reply); ok {
					break
				}
			}
		} else if ret < 0 {

		}
		// DoMapTask return 0, there is not IDLE map task, try DoReduceTask
	}

}

//
// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
//
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

//
// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
