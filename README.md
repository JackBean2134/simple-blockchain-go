golang 实现简易的区块链(Bitcoin)
# simple-blockchain-go
Golang实现简易区块链，作业项目
功能：
1. PoW工作量证明挖矿
2. 区块打包交易
3. bbolt数据库持久化存储区块
4. PoW有效性校验，区块迭代器反向遍历

### 运行方式
```bash
# 拉取依赖
go get go.etcd.io/bbolt
go mod tidy

# 运行程序
go run main.go
