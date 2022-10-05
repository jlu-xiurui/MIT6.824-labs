package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
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
func DoMapTask(mapf func(string, string) []KeyValue) (int, bool) {
	// call RPC to get content for Map
	path, err := os.Getwd()
	if err != nil {
		//fmt.Printf("Getwd failed!\n")
		return -1, false
	}
	args := AskMapArgs{Machine{path}}
	reply := AskMapReply{}
	ok := call("Coordinator.AskMapTask", &args, &reply)
	if !ok {
		//fmt.Printf("Call AskMapTask failed!\n")
		return -1, false
	}
	// All Map task is busy or over
	if reply.TaskId == -1 {
		//fmt.Printf("All map task busy or over!\n")
		return -1, reply.Over
	}
	// Do the user Map
	taskId := reply.TaskId
	nReduce := reply.NReduce
	//fmt.Printf("task %d content : %s,filename : %s", taskId, reply.Content, reply.Filename)
	filePath := reply.Path + "/" + reply.Filename
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("cannot open %v", filePath)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", filePath)
	}
	file.Close()
	kva := mapf(reply.Filename, string(content))
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
				//fmt.Printf("Encode failed!\n")
				return -1, false
			}
		}
		file.Close()
	}
	return taskId, false
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

func DoReduceTask(reducef func(string, []string) string) (int, bool) {
	// call RPC to get intermediate file for Reduce
	path, err := os.Getwd()
	if err != nil {
		//fmt.Printf("Getwd failed!\n")
		return -1, false
	}
	args := AskReduceArgs{Machine{path}}
	reply := AskReduceReply{}
	ok := call("Coordinator.AskReduceTask", &args, &reply)
	if !ok {
		//fmt.Printf("Call AskReduceTask failed!\n")
		return -1, false
	}
	// All Reduce task is busy or over
	if reply.TaskId == -1 {
		//fmt.Printf("All reduce task busy or over!\n")
		return -1, reply.Over
	}
	// read all intermediate file
	intermediate := []KeyValue{}
	intermediateMachines := reply.IntermediateMachine
	taskId := reply.TaskId
	for i, machine := range intermediateMachines {
		filename := fmt.Sprintf("%s/mr-%d%d", machine.Path, i, taskId)
		file, err := os.Open(filename)
		if err != nil {
			//fmt.Printf("Open %s failed!\n", filename)
			return -1, false
		}
		dec := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			intermediate = append(intermediate, kv)
		}
	}
	sort.Sort(ByKey(intermediate))

	tmpfile, err := ioutil.TempFile(path, "tmp")
	if err != nil {
		//fmt.Printf("TempFile failed!\n")
		return -1, false
	}
	// call Reduce on each distinct key in intermediate[],
	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(tmpfile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}
	newpath := fmt.Sprintf("%s/mr-out-%d", path, taskId)
	err = os.Rename(tmpfile.Name(), newpath)
	if err != nil {
		//fmt.Printf("Rename failed!\n")
		return -1, false
	}
	tmpfile.Close()
	return taskId, false
}
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.

	// uncomment to send the Example RPC to the coordinator.
	// CallExample()'
	mapDone := false
	reduceDone := false
	for !mapDone || !reduceDone {
		// try DoMapTask, if OK or cause err, then continue and try another map task
		if !mapDone {
			if ret, Done := DoMapTask(mapf); ret >= 0 {
				args := TaskOverArgs{ret}
				reply := TaskOverReply{}

				for !call("Coordinator.MapOver", &args, &reply) {
				}
				//fmt.Printf("map task %d finished\n", ret)
			} else if Done {
				// DoMapTask return -1, there is a err, just try again, if return over == true,
				// then all map tasks have finished
				mapDone = true
			}
		} else {
			// try DoReduceTask
			if ret, Done := DoReduceTask(reducef); ret >= 0 {
				args := TaskOverArgs{ret}
				reply := TaskOverReply{}
				for !call("Coordinator.ReduceOver", &args, &reply) {
				}
				//fmt.Printf("reduce task %d finished\n", ret)
			} else if Done {
				// DoReduceTask return -1, there is a err, just try again, if return over == true,
				// then all map tasks have finished
				reduceDone = true
			}
		}
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
