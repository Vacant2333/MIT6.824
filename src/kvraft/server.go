package kvraft

import (
	"mit6.824/labgob"
	"mit6.824/labrpc"
	"mit6.824/raft"
	"sync"
	"sync/atomic"
	"time"
)

type Op struct {
	Type  string
	Key   string
	Value string
	Tag   int64
}

type KVServer struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	dead         int32
	maxraftstate int

	data      map[string]string // K/V数据库
	doneTags  map[int64]bool
	dontIndex map[int]int64
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	kv.mu.Lock()
	if kv.isLeader() {
		DPrintf("S[%v] start Get key[%v]\n", kv.me, args.Key)
		index, _, _ := kv.rf.Start(Op{
			Type: "Get",
			Key:  args.Key,
			Tag:  args.Tag,
		})
		kv.mu.Unlock()
		for {
			done, tag := kv.checkOpDone(index)
			//fmt.Println(done, tag)
			if done {
				if tag == args.Tag {
					reply.Err = OK
					reply.Value = kv.data[args.Key]
				} else {
					reply.Err = ErrWrongLeader
				}
				return
			}
			time.Sleep(checkOpDoneSleepTime)
		}
	} else {
		kv.mu.Unlock()
		reply.Err = ErrWrongLeader
	}
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	kv.mu.Lock()
	if kv.isLeader() {
		index, _, _ := kv.rf.Start(Op{
			Type:  args.Op,
			Key:   args.Key,
			Value: args.Value,
			Tag:   args.Tag,
		})
		DPrintf("S[%v] start %v index:[%v] key[%v] value[%v]\n", kv.me, args.Op, index, args.Key, args.Value)
		// 持续检查任务是否完成
		kv.mu.Unlock()
		for {
			done, tag := kv.checkOpDone(index)
			if done {
				if tag == args.Tag {
					reply.Err = OK
				} else {
					reply.Err = ErrWrongLeader
				}
				break
			}
			time.Sleep(checkOpDoneSleepTime)
		}
	} else {
		kv.mu.Unlock()
		reply.Err = ErrWrongLeader
	}
}

// 检查自己是否为Leader,Lock使用
func (kv *KVServer) isLeader() bool {
	_, isLeader := kv.rf.GetState()
	return isLeader
}

func (kv *KVServer) applier() {
	for kv.killed() == false {
		// todo:exit Ch,reach Snapshot
		msg := <-kv.applyCh
		command, _ := msg.Command.(Op)
		kv.mu.Lock()
		canSave := !kv.checkTag(command.Tag, true)
		kv.dontIndex[msg.CommandIndex] = command.Tag
		if command.Type == "Put" && canSave {
			kv.data[command.Key] = command.Value
		} else if command.Type == "Append" && canSave {
			if _, ok := kv.data[command.Key]; ok {
				kv.data[command.Key] += command.Value
			} else {
				kv.data[command.Key] = command.Value
			}
		}
		kv.mu.Unlock()
	}
}

// 检查Tag是否有记录过,Lock使用
func (kv *KVServer) checkTag(tag int64, save bool) bool {
	_, ok := kv.doneTags[tag]
	if save && !ok {
		// 不存在并且save为True,存入doneTags
		kv.doneTags[tag] = true
	}
	return ok
}

// 通过index检查任务是否完成,返回是否完成和任务的tag,Lock使用
func (kv *KVServer) checkOpDone(index int) (bool, int64) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	tag, ok := kv.dontIndex[index]
	return ok, tag
}

func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	labgob.Register(Op{})
	applyCh := make(chan raft.ApplyMsg)
	kv := &KVServer{
		me:           me,
		rf:           raft.Make(servers, me, persister, applyCh),
		applyCh:      applyCh,
		maxraftstate: maxraftstate,
		data:         make(map[string]string),
		doneTags:     make(map[int64]bool),
		dontIndex:    make(map[int]int64),
	}
	go kv.applier()
	return kv
}

func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}
