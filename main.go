package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"time"

	"go.etcd.io/bbolt"
	"golang.org/x/crypto/ripemd160"
)

const (
	targetBits         = 24
	maxNonce           = math.MaxInt64
	dbFile             = "blockchain.db"
	blocksBucket       = "blocks"
	utxoBucket         = "utxo" // 新增UTXO缓存bucket
	subsidy            = 10
	version            = byte(0x00)
	addressChecksumLen = 4
	walletFile         = "wallets.dat"
)

// Wallet 钱包：不再直接存ecdsa.PrivateKey，存储密钥原始大数
type Wallet struct {
	D    *big.Int // 私钥
	X, Y *big.Int // 公钥坐标
}

// Wallets 钱包集合，持久化到文件
type Wallets struct {
	Wallets map[string]*Wallet
}

// NewWallet 创建新钱包，生成ECDSA密钥对
func NewWallet() *Wallet {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Panic(err)
	}

	return &Wallet{
		D: priv.D,
		X: priv.PublicKey.X,
		Y: priv.PublicKey.Y,
	}
}

// GetPrivateKey 还原ecdsa私钥对象
func (w *Wallet) GetPrivateKey() *ecdsa.PrivateKey {
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     w.X,
			Y:     w.Y,
		},
		D: w.D,
	}
}

// GetPublicKey 把公钥转为[]byte
func (w *Wallet) GetPublicKey() []byte {
	pub := w.GetPrivateKey().PublicKey
	return elliptic.Marshal(pub.Curve, pub.X, pub.Y)
}

// GetAddress 由公钥生成Base58比特币地址
func (w *Wallet) GetAddress() []byte {
	pubKeyHash := HashPubKey(w.GetPublicKey())

	versionedPayload := append([]byte{version}, pubKeyHash...)
	checksum := checksum(versionedPayload)

	fullPayload := append(versionedPayload, checksum...)
	address := Base58Encode(fullPayload)
	return address
}

// HashPubKey RIPEMD160(SHA256(pubkey))
func HashPubKey(pubKey []byte) []byte {
	publicSHA256 := sha256.Sum256(pubKey)
	RIPEMD160Hasher := ripemd160.New()
	_, err := RIPEMD160Hasher.Write(publicSHA256[:])
	if err != nil {
		log.Panic(err)
	}
	publicRIPEMD160 := RIPEMD160Hasher.Sum(nil)
	return publicRIPEMD160
}

// checksum 双重sha256，取前4字节
func checksum(payload []byte) []byte {
	firstSHA := sha256.Sum256(payload)
	secondSHA := sha256.Sum256(firstSHA[:])
	return secondSHA[:addressChecksumLen]
}

// Base58编码
func Base58Encode(input []byte) []byte {
	alphabet := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	var result []byte

	x := big.NewInt(0).SetBytes(input)
	base := big.NewInt(int64(len(alphabet)))
	zero := big.NewInt(0)
	mod := &big.Int{}

	for x.Cmp(zero) != 0 {
		x.DivMod(x, base, mod)
		result = append(result, alphabet[mod.Int64()])
	}

	// 反转
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	// 处理前导0
	for _, b := range input {
		if b == 0x00 {
			result = append([]byte{alphabet[0]}, result...)
		} else {
			break
		}
	}

	return result
}

// Base58解码
func Base58Decode(input []byte) []byte {
	alphabet := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	result := big.NewInt(0)
	for _, b := range input {
		charIndex := bytes.IndexByte([]byte(alphabet), b)
		if charIndex == -1 {
			log.Panic("base58 decode error")
		}
		result.Mul(result, big.NewInt(58))
		result.Add(result, big.NewInt(int64(charIndex)))
	}

	decoded := result.Bytes()

	// 前导1还原为0
	var leadingZeros int
	for _, b := range input {
		if b == '1' {
			leadingZeros++
		} else {
			break
		}
	}
	payload := append(bytes.Repeat([]byte{0x00}, leadingZeros), decoded...)
	return payload
}

// LoadWallets 从文件加载钱包集合
func LoadWallets() (*Wallets, error) {
	ws := Wallets{
		Wallets: make(map[string]*Wallet),
	}

	if _, err := os.Stat(walletFile); os.IsNotExist(err) {
		return &ws, nil
	}

	file, err := os.Open(walletFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&ws)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// SaveWallets 保存钱包集合到文件
func (ws Wallets) SaveWallets() {
	file, err := os.OpenFile(walletFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Panic(err)
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(ws)
	if err != nil {
		log.Panic(err)
	}
}

// CreateWallet 创建钱包并保存，返回地址
func (ws *Wallets) CreateWallet() string {
	wallet := NewWallet()
	address := string(wallet.GetAddress())
	ws.Wallets[address] = wallet
	return address
}

// GetWallet 获取指定地址对应的钱包，返回指针，找不到返回nil
func (ws *Wallets) GetWallet(address string) *Wallet {
	wallet, exists := ws.Wallets[address]
	if !exists {
		return nil
	}
	return wallet
}

// TXInput 交易输入：Signature签名，PubKey原始公钥
type TXInput struct {
	Txid      []byte
	Vout      int
	Signature []byte
	PubKey    []byte
}

// UsesKey 判断输入的公钥哈希是否匹配目标公钥哈希
func (in *TXInput) UsesKey(pubKeyHash []byte) bool {
	lockingHash := HashPubKey(in.PubKey)
	return bytes.Compare(lockingHash, pubKeyHash) == 0
}

// TXOutput 交易输出：金额 + 公钥哈希
type TXOutput struct {
	Value      int
	PubKeyHash []byte
}

// Lock 将输出锁定到某个地址
func (out *TXOutput) Lock(address []byte) {
	pubKeyHash := Base58Decode(address)
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	out.PubKeyHash = pubKeyHash
}

// IsLockedWithKey 判断输出是否由指定公钥哈希锁定
func (out *TXOutput) IsLockedWithKey(pubKeyHash []byte) bool {
	return bytes.Compare(out.PubKeyHash, pubKeyHash) == 0
}

// TXOutputs 用于序列化多个TXOutput，存入bbolt UTXO bucket
type TXOutputs struct {
	Outputs []TXOutput
}

// Serialize 序列化TXOutputs
func (outs TXOutputs) Serialize() []byte {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	err := enc.Encode(outs)
	if err != nil {
		log.Panic(err)
	}
	return buff.Bytes()
}

// DeserializeOutputs 反序列化TXOutputs
func DeserializeOutputs(data []byte) TXOutputs {
	var outs TXOutputs
	dec := gob.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&outs)
	if err != nil {
		log.Panic(err)
	}
	return outs
}

// Transaction 交易
type Transaction struct {
	ID   []byte
	Vin  []TXInput
	Vout []TXOutput
}

// IsCoinbase 判断是否为Coinbase挖矿交易
func (tx *Transaction) IsCoinbase() bool {
	return len(tx.Vin) == 1 && len(tx.Vin[0].Txid) == 0 && tx.Vin[0].Vout == -1
}

// Hash 计算交易哈希
func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	txCopy.ID = []byte{}

	hash = sha256.Sum256(txCopy.Serialize())
	return hash[:]
}

// Serialize 序列化交易
func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer
	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}
	return encoded.Bytes()
}

// TrimmedCopy 获取交易修剪副本，用于签名（清空Signature、PubKey）
func (tx *Transaction) TrimmedCopy() Transaction {
	var inputs []TXInput
	var outputs []TXOutput

	for _, vin := range tx.Vin {
		inputs = append(inputs, TXInput{vin.Txid, vin.Vout, nil, nil})
	}
	for _, vout := range tx.Vout {
		outputs = append(outputs, TXOutput{vout.Value, vout.PubKeyHash})
	}

	txCopy := Transaction{tx.ID, inputs, outputs}
	return txCopy
}

// Sign 对交易签名
func (tx *Transaction) Sign(privKey ecdsa.PrivateKey, prevTXs map[string]Transaction) {
	if tx.IsCoinbase() {
		return
	}
	txCopy := tx.TrimmedCopy()

	for inID, vin := range txCopy.Vin {
		prevTx := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Vin[inID].PubKey = nil

		r, s, err := ecdsa.Sign(rand.Reader, &privKey, txCopy.ID)
		if err != nil {
			log.Panic(err)
		}
		signature := append(r.Bytes(), s.Bytes()...)
		tx.Vin[inID].Signature = signature
	}
}

// Verify 校验交易签名
func (tx *Transaction) Verify(prevTXs map[string]Transaction) bool {
	txCopy := tx.TrimmedCopy()
	curve := elliptic.P256()

	for inID, vin := range tx.Vin {
		prevTx := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Vin[inID].PubKey = nil

		r := big.Int{}
		s := big.Int{}
		sigLen := len(vin.Signature)
		r.SetBytes(vin.Signature[:(sigLen / 2)])
		s.SetBytes(vin.Signature[(sigLen / 2):])

		x := big.Int{}
		y := big.Int{}
		keyLen := len(vin.PubKey)
		x.SetBytes(vin.PubKey[:(keyLen / 2)])
		y.SetBytes(vin.PubKey[(keyLen / 2):])

		rawPubKey := ecdsa.PublicKey{curve, &x, &y}
		if ecdsa.Verify(&rawPubKey, txCopy.ID, &r, &s) == false {
			return false
		}
	}
	return true
}

// NewCoinbaseTX 创建Coinbase交易
func NewCoinbaseTX(to, data string) *Transaction {
	if data == "" {
		data = fmt.Sprintf("Reward to '%s'", to)
	}
	txin := TXInput{[]byte{}, -1, nil, []byte(data)}
	txout := TXOutput{subsidy, nil}
	txout.Lock([]byte(to))
	tx := Transaction{nil, []TXInput{txin}, []TXOutput{txout}}
	tx.ID = tx.Hash()
	return &tx
}

// NewUTXOTransaction 创建普通转账交易，自动收集UTXO、签名
func NewUTXOTransaction(from, to string, amount int, utxoSet *UTXOSet) *Transaction {
	var inputs []TXInput
	var outputs []TXOutput

	wallets, err := LoadWallets()
	if err != nil {
		log.Panic(err)
	}
	wallet := wallets.GetWallet(from)
	if wallet == nil {
		log.Panicf("ERROR: 转出地址 %s 不存在", from)
	}
	pubKeyHash := HashPubKey(wallet.GetPublicKey())

	acc, validOutputs := utxoSet.FindSpendableOutputs(pubKeyHash, amount)
	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}

	// 构建输入
	for txid, outs := range validOutputs {
		txID, err := hex.DecodeString(txid)
		if err != nil {
			log.Panic(err)
		}
		for _, out := range outs {
			input := TXInput{txID, out, nil, wallet.GetPublicKey()}
			inputs = append(inputs, input)
		}
	}

	// 转账输出
	outputs = append(outputs, TXOutput{amount, nil})
	outputs[0].Lock([]byte(to))

	// 找零输出
	if acc > amount {
		outputs = append(outputs, TXOutput{acc - amount, nil})
		outputs[1].Lock([]byte(from))
	}

	tx := Transaction{nil, inputs, outputs}
	tx.ID = tx.Hash()
	utxoSet.Blockchain.SignTransaction(&tx, *wallet.GetPrivateKey())
	return &tx
}

// Merkle 树相关新增
type MerkleTree struct {
	RootNode *MerkleNode
}
type MerkleNode struct {
	Left  *MerkleNode
	Right *MerkleNode
	Data  []byte
}

func NewMerkleNode(left, right *MerkleNode, data []byte) *MerkleNode {
	mNode := MerkleNode{}
	if left == nil && right == nil {
		hash := sha256.Sum256(data)
		mNode.Data = hash[:]
	} else {
		prevHashes := append(left.Data, right.Data...)
		hash := sha256.Sum256(prevHashes)
		mNode.Data = hash[:]
	}
	mNode.Left = left
	mNode.Right = right
	return &mNode
}

func NewMerkleTree(data [][]byte) *MerkleTree {
	var nodes []MerkleNode
	if len(data)%2 != 0 {
		data = append(data, data[len(data)-1])
	}
	for _, datum := range data {
		node := NewMerkleNode(nil, nil, datum)
		nodes = append(nodes, *node)
	}
	for i := 0; i < len(nodes)/2; i++ {
		var newLevel []MerkleNode
		for j := 0; j < len(nodes); j += 2 {
			node := NewMerkleNode(&nodes[j], &nodes[j+1], nil)
			newLevel = append(newLevel, *node)
		}
		nodes = newLevel
	}
	mTree := MerkleTree{&nodes[0]}
	return &mTree
}

// Block 区块
type Block struct {
	Timestamp     int64
	Transactions  []*Transaction
	PrevBlockHash []byte
	Hash          []byte
	Nonce         int
}

// HashTransactions 修改：使用Merkle树根哈希
func (b *Block) HashTransactions() []byte {
	var txData [][]byte
	for _, tx := range b.Transactions {
		txData = append(txData, tx.Serialize())
	}
	tree := NewMerkleTree(txData)
	return tree.RootNode.Data
}

// NewBlock 创建新区块
func NewBlock(transactions []*Transaction, prevBlockHash []byte) *Block {
	block := &Block{time.Now().Unix(), transactions, prevBlockHash, []byte{}, 0}
	pow := NewProofOfWork(block)
	nonce, hash := pow.Run()

	block.Hash = hash[:]
	block.Nonce = nonce
	return block
}

// NewGenesisBlock 创建创世区块
func NewGenesisBlock(coinbase *Transaction) *Block {
	return NewBlock([]*Transaction{coinbase}, []byte{})
}

// Serialize 序列化区块
func (b *Block) Serialize() []byte {
	var result bytes.Buffer
	encoder := gob.NewEncoder(&result)
	err := encoder.Encode(b)
	if err != nil {
		log.Panic(err)
	}
	return result.Bytes()
}

// DeserializeBlock 反序列化区块
func DeserializeBlock(d []byte) *Block {
	var block Block
	decoder := gob.NewDecoder(bytes.NewReader(d))
	err := decoder.Decode(&block)
	if err != nil {
		log.Panic(err)
	}
	return &block
}

// ProofOfWork 工作量证明
type ProofOfWork struct {
	block  *Block
	target *big.Int
}

func NewProofOfWork(b *Block) *ProofOfWork {
	target := big.NewInt(1)
	target.Lsh(target, uint(256-targetBits))
	pow := &ProofOfWork{b, target}
	return pow
}

func IntToHex(num int64) []byte {
	buff := new(bytes.Buffer)
	err := binary.Write(buff, binary.BigEndian, num)
	if err != nil {
		log.Panic(err)
	}
	return buff.Bytes()
}

func (pow *ProofOfWork) prepareData(nonce int) []byte {
	data := bytes.Join(
		[][]byte{
			pow.block.PrevBlockHash,
			pow.block.HashTransactions(),
			IntToHex(pow.block.Timestamp),
			IntToHex(int64(targetBits)),
			IntToHex(int64(nonce)),
		},
		[]byte{},
	)
	return data
}

func (pow *ProofOfWork) Run() (int, []byte) {
	var hashInt big.Int
	var hash [32]byte
	nonce := 0

	fmt.Printf("Mining the block containing \"%s\"\n", pow.block.Transactions[0].Vin[0].PubKey)
	for nonce < maxNonce {
		data := pow.prepareData(nonce)
		hash = sha256.Sum256(data)
		fmt.Printf("\r%x", hash)
		hashInt.SetBytes(hash[:])
		if hashInt.Cmp(pow.target) == -1 {
			break
		} else {
			nonce++
		}
	}
	fmt.Print("\n\n")
	return nonce, hash[:]
}

func (pow *ProofOfWork) Validate() bool {
	var hashInt big.Int
	data := pow.prepareData(pow.block.Nonce)
	hash := sha256.Sum256(data)
	hashInt.SetBytes(hash[:])
	return hashInt.Cmp(pow.target) == -1
}

// Blockchain 区块链
type Blockchain struct {
	tip []byte
	db  *bbolt.DB
}

// UTXOSet UTXO缓存集合
type UTXOSet struct {
	Blockchain *Blockchain
}

func (u UTXOSet) Reindex() {
	db := u.Blockchain.db
	bucketName := []byte(utxoBucket)

	err := db.Update(func(tx *bbolt.Tx) error {
		_ = tx.DeleteBucket(bucketName)
		_, err := tx.CreateBucket(bucketName)
		return err
	})
	if err != nil {
		log.Panic(err)
	}

	UTXO := u.Blockchain.FindUTXOAll()

	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		for txID, outs := range UTXO {
			key, err := hex.DecodeString(txID)
			if err != nil {
				log.Panic(err)
			}
			err = b.Put(key, outs.Serialize())
			if err != nil {
				log.Panic(err)
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}

func (u UTXOSet) FindSpendableOutputs(pubkeyHash []byte, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	accumulated := 0
	db := u.Blockchain.db

	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			txID := hex.EncodeToString(k)
			outs := DeserializeOutputs(v)
			for outIdx, out := range outs.Outputs {
				if out.IsLockedWithKey(pubkeyHash) && accumulated < amount {
					accumulated += out.Value
					unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return accumulated, unspentOutputs
}

func (u UTXOSet) FindUTXO(pubKeyHash []byte) []TXOutput {
	var UTXOs []TXOutput
	db := u.Blockchain.db
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			outs := DeserializeOutputs(v)
			for _, out := range outs.Outputs {
				if out.IsLockedWithKey(pubKeyHash) {
					UTXOs = append(UTXOs, out)
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return UTXOs
}

func (u UTXOSet) Update(block *Block) {
	db := u.Blockchain.db
	err := db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		for _, tx := range block.Transactions {
			if !tx.IsCoinbase() {
				for _, vin := range tx.Vin {
					outsBytes := b.Get(vin.Txid)
					outs := DeserializeOutputs(outsBytes)
					updatedOuts := TXOutputs{}
					for outIdx, out := range outs.Outputs {
						if outIdx != vin.Vout {
							updatedOuts.Outputs = append(updatedOuts.Outputs, out)
						}
					}
					if len(updatedOuts.Outputs) == 0 {
						_ = b.Delete(vin.Txid)
					} else {
						_ = b.Put(vin.Txid, updatedOuts.Serialize())
					}
				}
			}
			newOutputs := TXOutputs{}
			for _, out := range tx.Vout {
				newOutputs.Outputs = append(newOutputs.Outputs, out)
			}
			_ = b.Put(tx.ID, newOutputs.Serialize())
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}

// BlockchainIterator 迭代器，从链尾向前遍历区块
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

// CreateBlockchain 创建区块链，生成创世块
func CreateBlockchain(address string) *Blockchain {
	db, err := bbolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	var tip []byte
	err = db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte(blocksBucket))
		if err != nil {
			log.Panic("Blockchain already exists.")
		}
		coinbaseTx := NewCoinbaseTX(address, "")
		genesis := NewGenesisBlock(coinbaseTx)
		err = bucket.Put(genesis.Hash, genesis.Serialize())
		if err != nil {
			log.Panic(err)
		}
		err = bucket.Put([]byte("l"), genesis.Hash)
		tip = genesis.Hash
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	bc := Blockchain{tip, db}
	return &bc
}

// BlockchainInstance 打开已存在的区块链
func BlockchainInstance() *Blockchain {
	db, err := bbolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	var tip []byte
	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		tip = b.Get([]byte("l"))
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	bc := Blockchain{tip, db}
	return &bc
}

// MineBlock 挖矿打包交易，写入数据库，打包前校验所有交易签名
func (bc *Blockchain) MineBlock(transactions []*Transaction) *Block {
	var lastHash []byte
	for _, tx := range transactions {
		if bc.VerifyTransaction(tx) != true {
			log.Panic("ERROR: Invalid transaction")
		}
	}

	err := bc.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l"))
		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	newBlock := NewBlock(transactions, lastHash)
	err = bc.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		err := b.Put(newBlock.Hash, newBlock.Serialize())
		if err != nil {
			log.Panic(err)
		}
		err = b.Put([]byte("l"), newBlock.Hash)
		bc.tip = newBlock.Hash
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return newBlock
}

// FindUnspentTransactions 遍历链，查找地址所有未花费交易
func (bc *Blockchain) FindUnspentTransactions(pubKeyHash []byte) []Transaction {
	var unspentTXs []Transaction
	spentTXOs := make(map[string][]int)
	bci := bc.Iterator()

	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)

		Outputs:
			for outIdx, out := range tx.Vout {
				if spentTXOs[txID] != nil {
					for _, spentOutIdx := range spentTXOs[txID] {
						if spentOutIdx == outIdx {
							continue Outputs
						}
					}
				}
				if out.IsLockedWithKey(pubKeyHash) {
					unspentTXs = append(unspentTXs, *tx)
				}
			}
			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin {
					if in.UsesKey(pubKeyHash) {
						inTxID := hex.EncodeToString(in.Txid)
						spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Vout)
					}
				}
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return unspentTXs
}

// FindUTXOAll 返回 map[txID][]TXOutput，给UTXOSet.Reindex使用
func (bc *Blockchain) FindUTXOAll() map[string]TXOutputs {
	UTXO := make(map[string]TXOutputs)
	spentTXOs := make(map[string][]int)
	bci := bc.Iterator()

	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)
			var outputs TXOutputs

		Outputs:
			for outIdx, out := range tx.Vout {
				// 如果这个输出已经被花费，跳过
				for _, spentOut := range spentTXOs[txID] {
					if spentOut == outIdx {
						continue Outputs
					}
				}
				outputs.Outputs = append(outputs.Outputs, out)
			}
			if len(outputs.Outputs) > 0 {
				UTXO[txID] = outputs
			}
			if !tx.IsCoinbase() {
				for _, vin := range tx.Vin {
					inTxID := hex.EncodeToString(vin.Txid)
					spentTXOs[inTxID] = append(spentTXOs[inTxID], vin.Vout)
				}
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return UTXO
}

// FindUTXO 获取地址全部UTXO，用于余额查询（现在优先用UTXOSet.FindUTXO）
func (bc *Blockchain) FindUTXO(pubKeyHash []byte) []TXOutput {
	var UTXOs []TXOutput
	unspentTransactions := bc.FindUnspentTransactions(pubKeyHash)
	for _, tx := range unspentTransactions {
		for _, out := range tx.Vout {
			if out.IsLockedWithKey(pubKeyHash) {
				UTXOs = append(UTXOs, out)
			}
		}
	}
	return UTXOs
}

// FindTransaction 根据交易ID查找交易
func (bc *Blockchain) FindTransaction(ID []byte) (Transaction, error) {
	bci := bc.Iterator()
	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			if bytes.Compare(tx.ID, ID) == 0 {
				return *tx, nil
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return Transaction{}, errors.New("Transaction is not found")
}

// SignTransaction 查找前置交易并对交易签名
func (bc *Blockchain) SignTransaction(tx *Transaction, privKey ecdsa.PrivateKey) {
	prevTXs := make(map[string]Transaction)
	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	tx.Sign(privKey, prevTXs)
}

// VerifyTransaction 验证交易签名合法性
func (bc *Blockchain) VerifyTransaction(tx *Transaction) bool {
	prevTXs := make(map[string]Transaction)
	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	return tx.Verify(prevTXs)
}

// CLI 命令行工具
type CLI struct{}

func (cli *CLI) printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  createwallet - Generates a new wallet")
	fmt.Println("  createblockchain -address ADDRESS - Create a blockchain and send genesis reward to ADDRESS")
	fmt.Println("  getbalance -address ADDRESS - Get balance of ADDRESS")
	fmt.Println("  send -from FROM -to TO -amount AMOUNT - Send AMOUNT coins")
	fmt.Println("  printchain - Print all blocks")
}

func (cli *CLI) validateArgs() {
	if len(os.Args) < 2 {
		cli.printUsage()
		os.Exit(1)
	}
}

func (cli *CLI) Run() {
	cli.validateArgs()

	createWalletCmd := flag.NewFlagSet("createwallet", flag.ExitOnError)
	createBlockchainCmd := flag.NewFlagSet("createblockchain", flag.ExitOnError)
	getBalanceCmd := flag.NewFlagSet("getbalance", flag.ExitOnError)
	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)
	printChainCmd := flag.NewFlagSet("printchain", flag.ExitOnError)

	createBlockchainAddress := createBlockchainCmd.String("address", "", "address")
	getBalanceAddress := getBalanceCmd.String("address", "", "address")
	sendFrom := sendCmd.String("from", "", "source address")
	sendTo := sendCmd.String("to", "", "destination address")
	sendAmount := sendCmd.Int("amount", 0, "amount")

	switch os.Args[1] {
	case "createwallet":
		err := createWalletCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
		cli.createWallet()
	case "createblockchain":
		err := createBlockchainCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
		if *createBlockchainAddress == "" {
			createBlockchainCmd.Usage()
			os.Exit(1)
		}
		cli.createBlockchain(*createBlockchainAddress)
	case "getbalance":
		err := getBalanceCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
		if *getBalanceAddress == "" {
			getBalanceCmd.Usage()
			os.Exit(1)
		}
		cli.getBalance(*getBalanceAddress)
	case "send":
		err := sendCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
		if *sendFrom == "" || *sendTo == "" || *sendAmount <= 0 {
			sendCmd.Usage()
			os.Exit(1)
		}
		cli.send(*sendFrom, *sendTo, *sendAmount)
	case "printchain":
		err := printChainCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
		cli.printChain()
	default:
		cli.printUsage()
		os.Exit(1)
	}
}

func (cli *CLI) createWallet() {
	wallets, err := LoadWallets()
	if err != nil {
		log.Panic(err)
	}
	address := wallets.CreateWallet()
	wallets.SaveWallets()
	fmt.Printf("Your new address: %s\n", address)
}

func (cli *CLI) createBlockchain(address string) {
	bc := CreateBlockchain(address)
	defer bc.db.Close()
	utxoSet := UTXOSet{bc}
	utxoSet.Reindex()
	fmt.Println("Done!")
}

func (cli *CLI) getBalance(address string) {
	bc := BlockchainInstance()
	defer bc.db.Close()
	utxoSet := UTXOSet{bc}

	wallets, err := LoadWallets()
	if err != nil {
		log.Panic(err)
	}
	wallet := wallets.GetWallet(address)
	if wallet == nil {
		log.Panicf("ERROR: Address %s does not exist", address)
	}
	pubKeyHash := HashPubKey(wallet.GetPublicKey())

	UTXOs := utxoSet.FindUTXO(pubKeyHash)
	balance := 0
	for _, out := range UTXOs {
		balance += out.Value
	}
	fmt.Printf("Balance of '%s': %d\n", address, balance)
}

func (cli *CLI) send(from, to string, amount int) {
	bc := BlockchainInstance()
	defer bc.db.Close()
	utxoSet := UTXOSet{bc}

	wallets, err := LoadWallets()
	if err != nil {
		log.Panic(err)
	}
	wallet := wallets.GetWallet(from)
	if wallet == nil {
		log.Panicf("ERROR: from address %s not found", from)
	}

	// coinbase奖励交易 + 用户转账交易
	cbTx := NewCoinbaseTX(from, "")
	tx := NewUTXOTransaction(from, to, amount, &utxoSet)
	txs := []*Transaction{cbTx, tx}

	newBlock := bc.MineBlock(txs)
	utxoSet.Update(newBlock)
	fmt.Println("Success!")
}

func (cli *CLI) printChain() {
	bc := BlockchainInstance()
	defer bc.db.Close()

	bci := bc.Iterator()
	for {
		block := bci.Next()
		fmt.Printf("Prev. hash: %x\n", block.PrevBlockHash)
		fmt.Printf("Hash: %x\n", block.Hash)
		pow := NewProofOfWork(block)
		fmt.Printf("PoW: %t\n", pow.Validate())
		for _, tx := range block.Transactions {
			fmt.Println(tx)
		}
		fmt.Println()
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
}

func main() {
	gob.Register(Wallet{})
	cli := CLI{}
	cli.Run()
}
