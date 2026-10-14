# Go 简易区块链（Part5：钱包、ECDSA签名、比特币地址）
参考原文：https://learnblockchain.cn/article/637
> 在之前UTXO模型基础上升级：实现钱包、椭圆曲线ECDSA公私钥、Base58比特币地址、交易数字签名与交易合法性校验。
> 项目复现比特币身份体系：私钥拥有资产控制权，交易必须签名，节点验证签名后交易才能上链。

## ✨ 新增功能（对比上一版）
1. **钱包Wallet**：生成ECDSA公私钥对，支持创建多钱包，持久化保存到wallets.dat
2. **真实比特币地址算法**
   - 公钥哈希：`RIPEMD160(SHA256(PubKey))`
   - Base58编码（剔除容易混淆字符：0,O,I,l,+,/）
   - 版本前缀 + 公钥哈希 + 4字节校验和
3. **交易数字签名 ECDSA**
   - Coinbase交易无需签名
   - 普通转账使用私钥对修剪后的交易副本签名
   - 挖矿打包区块前校验所有交易签名，非法交易直接拒绝
4. 交易结构改造：移除ScriptPubKey/ScriptSig，替换为PubKeyHash、Signature、PubKey字段

> ⚠️ 教学简化项目，单机运行，无P2P网络。

## 📦 环境依赖
```bash
go get go.etcd.io/bbolt
go get golang.org/x/crypto/ripemd160
```
## 🚀 使用命令
重置项目：删除旧数据库与钱包文件
```powershell
# Windows
Remove-Item blockchain.db,wallets.dat -Force
# Linux/Mac
rm blockchain.db wallets.dat
```
```bash
# 1. 创建钱包，生成比特币风格地址
go run main.go createwallet

# 2. 创建区块链，创世块奖励发给某个地址
go run main.go createblockchain -address 13Uu7B1vDP4ViXqHFsWtbraM3EfQ3UkWXt

# 3. 查询地址余额
go run main.go getbalance -address 13Uu7B1vDP4ViXqHFsWtbraM3EfQ3UkWXt

# 4. 转账（必须使用本地钱包内存在的地址）
go run main.go send -from 1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR -to 1NE86r4Esjf53EL7fR86CsfTZpNN42Sfab -amount 6

# 5. 打印整条区块链
go run main.go printchain
```
## 🧩 核心概念
### 1、ECDSA 椭圆曲线加密
私钥随机生成；公钥由私钥推导。私钥代表资产所有权，只有私钥持有者才能签名交易，花费 UTXO。公钥可以公开。
#### 2、比特币地址生成流程
原始公钥 → SHA256 → RIPEMD160 → 添加版本字节 → 双重 SHA256 取前 4 字节作为校验和 → Base58 编码 → 最终地址。
地址是公钥的人类可读编码，地址公开，但无法反向推导出私钥。
### 3、交易签名机制
签名对象不是完整交易，而是修剪后的交易副本（清空输入的 Signature、PubKey）
签名使用私钥；验证使用公钥
挖矿前自动校验签名，签名不合法交易直接报错，无法打包上链
### 4、UTXO
资产存储在 TXOutput 输出。输出被公钥哈希锁定；转账时，使用钱包私钥签名，解锁旧 UTXO，生成新输出（转账 + 找零）。

## 📁 项目文件
```plaintext
blockchain/
├── main.go        # 全部源码
├── blockchain.db  # bbolt区块数据库
├── wallets.dat    # 钱包文件，保存公私钥
└── README.md

```
##  📌 项目局限
1、单机程序，无 P2P 网络节点广播
2、没有交易池，交易直接打包挖矿
3、钱包文件简单 gob 序列化，缺少加密保护
4、未实现完整比特币脚本系统

## License
仅供课程学习实验，禁止生产使用
```plaintext

## 运行示例
```powershell
Remove-Item blockchain.db,wallets.dat -Force

go run main.go createwallet
Your new address: 1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR

go run main.go createblockchain -address 1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR
Mining the block containing "Reward to '1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR'"
000000122348da06c19e5c513710340f4c307d884385da948a205655c6a9d008
Done!

go run main.go send -from 1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR -to 1NE86r4Esjf53EL7fR86CsfTZpNN42Sfab -amount 6
Mining the block containing "Reward to '1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR'"
0000000f3dbb0ab6d56c4e4b9f7479afe8d5a5dad4d2a8823345a1a16cf3347b
Success!

go run main.go getbalance -address 1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR
Balance of '1AmVdDvvQ977oVCpUqz7zAPUEiXKrX5avR': 4

go run main.go getbalance -address 1NE86r4Esjf53EL7fR86CsfTZpNN42Sfab
Balance of '1NE86r4Esjf53EL7fR86CsfTZpNN42Sfab': 6

```
```markdown
# blockchain_go Part6
基于Jeiwan区块链教程 Part6，单文件Go实现：UTXO缓存集 + Merkle Tree + P2PKH

## 功能
1. Coinbase挖矿奖励交易
2. UTXO Set 缓存（bbolt独立bucket，避免每次查询全链遍历）
3. Merkle Tree，区块头存储Merkle根，支持SPV基础
4. P2PKH 地址、钱包、ECDSA签名、交易验证

## 命令
# 创建钱包

go run main.go createwallet

# 创建区块链（创世块）
```bash
go run main.go createblockchain -address 你的地址
```
# 转账（自带 coinbase 挖矿奖励）
```bash
go run main.go send -from 源地址 -to 目标地址 -amount 数量
```
# 查询余额
```bash
go run main.go getbalance -address 地址
```
# 打印整条链
```bash
go run main.go printchain
```
```