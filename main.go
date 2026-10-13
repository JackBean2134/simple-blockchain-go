package main

// 主程序入口
import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"
)

type Block struct {
	Timestamp int64  //当前时间戳，也就是区块创建的时间
	Data      []byte //区块存储的实际有效信息，也就是交易
	PrevBlock []byte //前一个块的哈希，即父哈希
	Hash      []byte //当前块的哈希
}

type Blockchain struct {
	Blocks []*Block
}

func (block *Block) SetHash() {
	timestamp := []byte(strconv.FormatInt(block.Timestamp, 10))
	header := bytes.Join([][]byte{block.PrevBlock, block.Data, timestamp}, []byte{})
	hash := sha256.Sum256(header)
	block.Hash = hash[:]

}

// 创建简化区块
func NewBlock(data string, prevBlockHash []byte) *Block {
	block := &Block{time.Now().Unix(), []byte(data), prevBlockHash, []byte{}}
	block.SetHash()
	return block
}

// 添加区块
func (bc *Blockchain) AddBlock(data string) {
	prevBlock := bc.Blocks[len(bc.Blocks)-1]
	block := NewBlock(data, prevBlock.Hash)
	bc.Blocks = append(bc.Blocks, block)
}

// 创建创世块的方法
func NewGenesisBlock() *Block {
	return NewBlock("Genesis Block", []byte{})
}

// 实现一个函数创建有创世块的区块链
func NewBlockchain() *Blockchain {
	return &Blockchain{[]*Block{NewGenesisBlock()}}
}
func main() {
	bc := NewBlockchain()

	bc.AddBlock("Send 1 BTC to Ivan")
	bc.AddBlock("Send 2 more BTC to Ivan")

	for _, block := range bc.Blocks {
		fmt.Printf("Previous hash: %x\n", block.PrevBlock)
		fmt.Printf("Data: %s\n", block.Data)
		fmt.Printf("Hash: %x\n", block.Hash)
		fmt.Println()
	}

}
