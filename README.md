📖 项目简介
本项目是一个教学用途的简易区块链实现，参考比特币核心原理，采用 UTXO（未花费交易输出）模型，不使用传统账户余额体系，币存储在交易输出（TXOutput）中。
核心特性：
✅ PoW 工作量证明：挖矿机制，计算哈希满足难度条件，保证区块不可篡改
✅ Coinbase 挖矿交易：创世区块内置 Coinbase 交易，无输入，凭空产生挖矿奖励
✅ UTXO 交易模型：TXInput（交易输入）引用历史未花费输出；TXOutput（交易输出）保存代币 + 锁定脚本
✅ 持久化存储：使用 bbolt（嵌入式 KV 数据库）保存区块数据，程序重启数据不丢失
✅ 命令行 CLI 工具：创建链、转账、查询余额、打印整条区块链
✅ 交易锁定 / 解锁脚本（简化版）：ScriptPubKey（输出锁定）、ScriptSig（输入解锁），使用地址字符串校验，未引入公私钥签名
⚠️ 项目为教学简化版本，不具备生产安全性：无真实非对称加密签名、无 P2P 网络、无默克尔树完整实现、无交易池。
📦 环境依赖
Go 1.18+
bbolt 嵌入式数据库
bash
# 安装依赖
go get go.etcd.io/bbolt
📁 项目结构
plaintext
blockchain-go/
├── main.go          # 全部代码：区块、交易、UTXO、PoW、CLI命令
├── blockchain.db    # bbolt数据库文件（运行后自动生成）
└── README.md        # 项目说明文档
🧩 核心数据结构说明
1. 交易输出 TXOutput
   代币存储载体，带有锁定脚本ScriptPubKey，规定谁可以花费这笔币。
   go
   运行
   type TXOutput struct {
   Value        int      // 金额
   ScriptPubKey string   // 锁定脚本（本项目简化为地址字符串）
   }
2. 交易输入 TXInput
   消费旧交易输出，引用之前交易的输出，附带解锁脚本ScriptSig。
   go
   运行
   type TXInput struct {
   Txid      []byte   // 引用的旧交易ID
   Vout      int      // 旧交易输出索引
   ScriptSig string   // 解锁脚本（简化为地址字符串）
   }
3. 交易 Transaction
   一笔交易包含多个输入与输出。
   Coinbase 交易：挖矿奖励交易，没有真实输入，Txid=[]byte{}, Vout=-1
   go
   运行
   type Transaction struct {
   ID   []byte       // 交易哈希ID
   Vin  []TXInput    // 交易输入数组
   Vout []TXOutput   // 交易输出数组
   }
4. 区块 Block
   区块不再存储简单字符串 Data，而是存储一组交易。
   go
   运行
   type Block struct {
   Timestamp     int64          // 区块时间戳
   Transactions  []*Transaction // 区块内所有交易
   PrevBlockHash []byte         // 上一个区块哈希
   Hash          []byte         // 当前区块哈希
   Nonce         int            // PoW随机数
   }
   🚀 使用教程
   每次重新创建区块链前，必须删除旧数据库文件，否则会提示区块链已存在
   bash
# 删除旧数据库（PowerShell）
Remove-Item blockchain.db -Force

# Linux/Mac
rm blockchain.db
1. 创建区块链（生成创世区块 + Coinbase 挖矿奖励）
   将挖矿奖励 50 个币发放给地址 Ivan
   bash
   go run main.go createblockchain -address Ivan
2. 查询地址余额
   遍历区块链，查找该地址全部 UTXO，统计余额
   bash
   go run main.go getbalance -address Ivan
3. 转账交易
   Ivan 向 Tom 转账 10 个币，自动生成找零输出，打包进新区块并挖矿
   bash
   go run main.go send -from Ivan -to Tom -amount 10
4. 打印整条区块链
   遍历输出所有区块、交易、输入输出、PoW 有效性
   bash
   go run main.go printchain
   💡 核心原理介绍
1. Coinbase 交易（创币交易）
   仅出现在新区块，矿工挖矿奖励
   唯一没有引用之前交易输出的交易
   创世区块的第一笔交易就是 Coinbase，实现 “凭空发行代币”
2. UTXO 模型
   区块链没有账户，不存在 “账户余额” 字段。
   UTXO：Unspent Transaction Output，未花费的交易输出
   币保存在 TXOutput 中；当 TXOutput 被 TXInput 引用，标记为已花费，不能再次使用
   查询余额：遍历整条链，找到地址可解锁的所有未花费输出，累加金额
   转账：收集足够金额的 UTXO 作为输入，生成新的输出（转账金额 + 找零）
3. PoW 工作量证明
   不断改变 nonce 随机数，计算区块头哈希
   哈希值必须小于目标阈值（targetBits 控制难度）
   找到符合条件 nonce 即为挖矿成功，生成新区块
   验证区块：重新运行 PoW，校验哈希是否合法，保证区块数据不可篡改
4. 交易锁定 / 解锁（简化脚本）
   TXOutput.ScriptPubKey：锁定脚本，代表这笔币属于哪个地址
   TXInput.ScriptSig：解锁脚本，证明当前用户有权花费输出
   简化实现：直接对比地址字符串；真实比特币采用公钥 + ECDSA 数字签名
   📌 实验结果示例
   plaintext
# 创建链
go run main.go createblockchain -address Ivan
Mining a new block
00000052342345124...

Blockchain created!

# 查询余额
go run main.go getbalance -address Ivan
Balance of 'Ivan': 50

# 转账
go run main.go send -from Ivan -to Tom -amount 10
Mining a new block
00000088acb...
Send Success!

# 再次查询余额
go run main.go getbalance -address Ivan
Balance of 'Ivan': 40

go run main.go getbalance -address Tom
Balance of 'Tom': 10
📝 项目不足与后续改进方向
当前脚本仅字符串比对，未实现公私钥、数字签名，任何人都可以随便花费地址资产
无 P2P 网络，仅单机本地区块链，没有节点同步
交易池未实现，交易必须立刻打包挖矿
默克尔树简化，仅交易哈希拼接，没有完整 Merkle Tree 支持 SPV 简易支付验证
无区块持久化校验、无交易合法性更多校验（重复花费防护可进一步强化）
📄 许可证
本项目仅用于课程学习与实验，仅供教学参考。