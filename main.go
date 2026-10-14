package main

// 主程序入口
import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"log"
	"math"
	"math/big"
	"strconv"
	"time"

	"go.etcd.io/bbolt"
)

// 定义挖矿的难度值
const (
	targetBits   = 24
	dbFile       = "blockchain.db"
	blocksBucket = "blocks"
)

type ProofOfWork struct {
	block  *Block
	target *big.Int
}

func NewProofOfWork(b *Block) *ProofOfWork {
	target := big.NewInt(1)
	target.Lsh(target, uint(256-targetBits))

	return &ProofOfWork{b, target}
}

// IntToHex 将int64转为大端字节数组
func IntToHex(num int64) []byte {
	bigNum := big.NewInt(num)
	return bigNum.Bytes()
}
func (pow *ProofOfWork) prepareData(nonce int) []byte {
	data := bytes.Join(
		[][]byte{
			pow.block.PrevBlockHash,
			pow.block.Data,
			IntToHex(pow.block.Timestamp),
			IntToHex(int64(targetBits)),
			IntToHex(int64(nonce)),
		},
		[]byte{},
	)
	return data
}

// pow算法核心
func (pow *ProofOfWork) Run() (int, []byte) {
	var hashInt big.Int
	var hash [32]byte
	nonce := 0

	fmt.Printf("Mining the block containing %d transactions.\n", len(pow.block.Data))
	for nonce < math.MaxInt64 {
		data := pow.prepareData(nonce)
		hash = sha256.Sum256(data)
		hashInt.SetBytes(hash[:])
		if hashInt.Cmp(pow.target) == -1 {
			fmt.Printf("\r%x", hash)
			break
		} else {
			nonce++
		}
	}
	fmt.Println("\n\n")
	return nonce, hash[:]
}

// 对工作用量进行验证
func (pow *ProofOfWork) Validate() bool {
	var hashInt big.Int

	data := pow.prepareData(pow.block.Nonce)
	hash := sha256.Sum256(data)
	hashInt.SetBytes(hash[:])
	isValid := hashInt.Cmp(pow.target) == -1
	return isValid
}

type Block struct {
	Timestamp     int64  //当前时间戳，也就是区块创建的时间
	Data          []byte //区块存储的实际有效信息，也就是交易
	PrevBlockHash []byte //前一个块的哈希，即父哈希
	Hash          []byte //当前块的哈希
	Nonce         int
}

type Blockchain struct {
	tip []byte
	db  *bbolt.DB
}

// 区块链迭代器
type BlockchainIterator struct {
	currentHash []byte
	db          *bbolt.DB
}

func (bc *Blockchain) Iterator() *BlockchainIterator {
	bci := &BlockchainIterator{bc.tip, bc.db}

	return bci
}

func (i *BlockchainIterator) Next() *Block {
	var block *Block

	err := i.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		encodedBlock := b.Get(i.currentHash)
		block = DeserializeBlock(encodedBlock)

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	i.currentHash = block.PrevBlockHash

	return block
}

//func (block *Block) SetHash() {
//	timestamp := []byte(strconv.FormatInt(block.Timestamp, 10))
//	header := bytes.Join([][]byte{block.PrevBlockHash, block.Data, timestamp}, []byte{})
//	hash := sha256.Sum256(header)
//	block.Hash = hash[:]
//
//}

// 创建简化区块
func NewBlock(data string, prevBlockHash []byte) *Block {
	block := &Block{time.Now().Unix(), []byte(data), prevBlockHash, []byte{}, 0}
	pow := NewProofOfWork(block)
	nonce, hash := pow.Run()
	block.Hash = hash
	block.Nonce = nonce
	return block
}

// 添加区块
/*更新 AddBlock 方法：现在向链中加入区块，
就不是像之前向一个数组中加入一个元素那么简单了。
从现在开始，我们会将区块存储在数据库里面
*/
func (bc *Blockchain) AddBlock(data string) {
	var lastHash []byte

	err := bc.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l"))
		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	newBlock := NewBlock(data, lastHash)

	err = bc.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		err1 := b.Put(newBlock.Hash, newBlock.Serialize())
		if err1 != nil {
			return err1
		}
		err = b.Put([]byte("l"), newBlock.Hash)
		bc.tip = newBlock.Hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}

// 创建创世块的方法
func NewGenesisBlock() *Block {
	return NewBlock("Genesis Block", []byte{})
}

// 实现一个函数创建有创世块的区块链 持久化

/*
打开一个数据库文件
检查文件里面是否已经存储了一个区块链
如果已经存储了一个区块链：
创建一个新的 Blockchain 实例
设置 Blockchain 实例的 tip 为数据库中存储的最后一个块的哈希
如果没有区块链：
创建创世块
存储到数据库
将创世块哈希保存为最后一个块的哈希
创建一个新的 Blockchain 实例，初始时 tip 指向创世块（tip 有尾部，尖端的意思，在这里 tip 存储的是最后一个块的哈希）
*/
func NewBlockchain() *Blockchain {
	var tip []byte
	db, err := bbolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		//bucket 不存在，新建
		if b == nil {
			fmt.Println("No existing blockchain found. Creating genesis block...")
			var err error
			b, err = tx.CreateBucket([]byte(blocksBucket)) // 只用 = ，复用外层b，不是 :=
			if err != nil {
				return err
			}
			genesis := NewGenesisBlock()
			err = b.Put(genesis.Hash, genesis.Serialize())
			if err != nil {
				return err
			}
			err = b.Put([]byte("l"), genesis.Hash)
			if err != nil {
				return err
			}
			tip = genesis.Hash
		} else {
			// bucket存在，读取最后区块哈希
			tip = b.Get([]byte("l"))
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	bc := Blockchain{tip, db}
	return &bc
}

// 实现Block的Serialize方法
func (b *Block) Serialize() []byte {
	var result bytes.Buffer
	encoder := gob.NewEncoder(&result)

	err := encoder.Encode(b)
	if err != nil {
		log.Panic(err)
	}
	return result.Bytes()
}

// 解序列化的函数
func DeserializeBlock(data []byte) *Block {
	var block Block
	decoder := gob.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(&block)
	if err != nil {
		log.Panic(err)
	}
	return &block
}

func main() {
	bc := NewBlockchain()

	bc.AddBlock("Send 1 BTC to Ivan")
	bc.AddBlock("Send 2 more BTC to Ivan")

	// 使用迭代器遍历链（不再用bc.Blocks）
	iter := bc.Iterator()
	for {
		block := iter.Next()

		fmt.Printf("Previous hash: %x\n", block.PrevBlockHash)
		fmt.Printf("Data: %s\n", block.Data)
		fmt.Printf("Hash: %x\n", block.Hash)

		pow := NewProofOfWork(block)
		fmt.Printf("PoW校验结果: %s\n", strconv.FormatBool(pow.Validate()))
		fmt.Println()

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

}
