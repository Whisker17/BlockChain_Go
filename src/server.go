package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
)

/*
*接收到 version 消息的节点应该做什么呢？它会响应自己的 version 消息。
*这是一种握手：如果没有事先互相问候，就不可能有其他交流。
*不过，这并不是出于礼貌：version 用于找到一个更长的区块链。当一个节点接收到 version 消息，它会检查本节点的区块链是否比 BestHeight 的值更大。
*如果不是，节点就会请求并下载缺失的块。
 */
const protocol = "tcp"
const nodeVersion = 1
const commandLength = 12

var nodeAddress string

//minerAddress 参数指定了接收挖矿奖励的地址
var miningAddress string
var knownNodes = []string{"localhost:3000"}
var blockInTransit = [][]byte{}
var mempool = make(map[string]Transaction)

type addr struct {
	AddrList []string
}

type block struct {
	AddrFrom string
	Block    []byte
}

type getblocks struct {
	AddrFrom string
}

//getdata 用于某个块或交易的请求，它可以仅包含一个块或交易的 ID
type getdata struct {
	AddrFrom string
	Type     string
	ID       []byte
}

//比特币使用 inv 来向其他节点展示当前节点有什么块和交易。再次提醒，它没有包含完整的区块链和交易，仅仅是哈希而已
//Type 字段表明了这是块还是交易
type inv struct {
	AddrFrom string
	Type     string
	Items    [][]byte
}

type tx struct {
	AddrFrom    string
	Transaction []byte
}

//BestHeight： 存储区块链中节点的高度。
//AddFrom： 存储发送者的地址。
type verzion struct {
	Version    int
	BestHeight int
	AddrFrom   string
}

//将string类型的命令转换成[]byte
func commandToBytes(command string) []byte {
	var bytes [commandLength]byte

	for i, c := range command {
		bytes[i] = byte(c)
	}

	return bytes[:]
}

//将[]byte转换成string类型
func bytesToCommand(bytes []byte) string {
	var command []byte

	for _, b := range bytes {
		if b != 0x0 {
			command = append(command, b)
		}
	}

	return fmt.Sprintf("%s", command)
}

//从[]byte中提取出命令
func extractCommand(request []byte) []byte {
	return request[:commandLength]
}

func requestBlocks() {
	for _, node := range knownNodes {
		sendGetBlocks(node)
	}
}

//发送地址信息
func sendAddr(address string) {
	nodes := addr{knownNodes}
	nodes.AddrList = append(nodes.AddrList, nodeAddress)
	payload := gobEncode(nodes)
	request := append(commandToBytes("addr"), payload...)

	sendData(address, request)
}

//发送块
func sendBlock(addr string, b *Block) {
	data := block{nodeAddress, b.Serialize()}
	payload := gobEncode(data)
	request := append(commandToBytes("block"), payload...)

	sendData(addr, request)
}

//发送数据
func sendData(addr string, data []byte) {
	//使用tcp方式与特定端口进行通信
	conn, err := net.Dial(protocol, addr)
	if err != nil {
		fmt.Printf("%s is not available\n", addr)
		var updatedNodes []string

		//更新地址
		for _, node := range knownNodes {
			if node != addr {
				updatedNodes = append(updatedNodes, node)
			}
		}

		knownNodes = updatedNodes

		return
	}
	defer conn.Close()

	_, err = io.Copy(conn, bytes.NewReader(data))
	if err != nil {
		log.Panic(err)
	}
}

//发送清单
func sendInv(address, kind string, items [][]byte) {
	inventory := inv{nodeAddress, kind, items}
	payload := gobEncode(inventory)
	request := append(commandToBytes("inv"), payload...)

	sendData(address, request)
}

func sendGetBlocks(address string) {
	payload := gobEncode(getblocks{nodeAddress})
	request := append(commandToBytes("getblocks"), payload...)

	sendData(address, request)
}

func sendGetData(address, kind string, id []byte) {
	payload := gobEncode(getdata{nodeAddress, kind, id})
	request := append(commandToBytes("getdata"), payload...)

	sendData(address, request)
}

//发送交易
func sendTx(addr string, tnx *Transaction) {
	data := tx{nodeAddress, tnx.Serialize()}
	payload := gobEncode(data)
	request := append(commandToBytes("tx"), payload...)

	sendData(addr, request)
}

//如果当前节点不是中心节点，它必须向中心节点发送 version 消息来查询是否自己的区块链已过时
func sendVersion(addr string, bc *Blockchain) {
	bestHeight := bc.GetBestHeight()
	payload := gobEncode(verzion{nodeVersion, bestHeight, nodeAddress})

	request := append(commandToBytes("version"), payload...)

	sendData(addr, request)
}

func handleAddr(request []byte) {
	//我们需要对请求进行解码，提取有效信息。所有的处理器在这部分都类似。
	var buff bytes.Buffer
	var payload addr

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	knownNodes = append(knownNodes, payload.AddrList...)
	fmt.Printf("There are %d known nodes now!\n", len(knownNodes))
	requestBlocks()
}

//当接收到一个新块时，我们把它放到区块链里面
//如果还有更多的区块需要下载，我们继续从上一个下载的块的那个节点继续请求
//当最后把所有块都下载完后，对 UTXO 集进行重新索引
func handleBlock(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload block

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	blockData := payload.Block
	block := DeserializeBlock(blockData)

	fmt.Println("Recevied a new block!")
	bc.AddBlock(block)

	fmt.Printf("Added block %x\n", block.Hash)

	if len(blockInTransit) > 0 {
		blockHash := blockInTransit[0]
		sendGetData(payload.AddrFrom, "block", blockHash)

		blockInTransit = blockInTransit[1:]
	} else {
		UTXOSet := UTXOSet{bc}
		//UTXOSet.Reindex()
		UTXOSet.Update(block)
	}
}

func handleInv(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload inv

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	fmt.Printf("Recevied inventory with %d %s\n", len(payload.Items), payload.Type)

	//如果收到块哈希，我们想要将它们保存在 blocksInTransit 变量来跟踪已下载的块。这能够让我们从不同的节点下载块
	//在将块置于传送状态时，我们给 inv 消息的发送者发送 getdata 命令并更新 blocksInTransit
	if payload.Type == "block" {
		blockInTransit = payload.Items

		blockHash := payload.Items[0]
		sendGetData(payload.AddrFrom, "block", blockHash)

		newInTransit := [][]byte{}
		for _, b := range blockInTransit {
			if bytes.Compare(b, blockHash) != 0 {
				newInTransit = append(newInTransit, b)
			}
		}
		blockInTransit = newInTransit
	}

	//在我们的实现中，我们永远也不会发送有多重哈希的 inv。
	//这就是为什么当 payload.Type == "tx" 时，只会拿到第一个哈希。
	//然后我们检查是否在内存池中已经有了这个哈希，如果没有，发送 getdata 消息。
	if payload.Type == "tx" {
		txID := payload.Items[0]

		if mempool[hex.EncodeToString(txID)].ID == nil {
			sendGetData(payload.AddrFrom, "tx", txID)
		}
	}
}

func handleGetBlocks(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload getblocks

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	blocks := bc.GetBlockHashes()
	sendInv(payload.AddrFrom, "block", blocks)
}

//如果它们请求一个块，则返回块；如果它们请求一笔交易，则返回交易。
func handleGetData(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload getdata

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	if payload.Type == "block" {
		block, err := bc.GetBlock([]byte(payload.ID))
		if err != nil {
			return
		}

		sendBlock(payload.AddrFrom, &block)
	}

	if payload.Type == "tx" {
		txID := hex.EncodeToString(payload.ID)
		tx := mempool[txID]

		sendTx(payload.AddrFrom, &tx)
	}
}

func handleTx(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload tx

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	//首先要做的事情是将新交易放到内存池中（再次提醒，在将交易放到内存池之前，必要对其进行验证）
	txData := payload.Transaction
	tx := DeserializeTransaction(txData)
	mempool[hex.EncodeToString(tx.ID)] = tx

	//检查当前节点是否是中心节点。在我们的实现中，中心节点并不会挖矿。它只会将新的交易推送给网络中的其他节点。
	if nodeAddress == knownNodes[0] {
		for _, node := range knownNodes {
			if node != nodeAddress && node != payload.AddrFrom {
				sendInv(node, "tx", [][]byte{tx.ID})
			}
		}
	} else {
		//miningAddress 只会在矿工节点上设置。
		//如果当前节点（矿工）的内存池中有两笔或更多的交易，开始挖矿
		if len(mempool) >= 2 && len(miningAddress) > 0 {
		MineTransactions:
			var txs []*Transaction

			//内存池中所有交易都是通过验证的。无效的交易会被忽略，如果没有有效交易，则挖矿中断
			for id := range mempool {
				tx := mempool[id]
				if bc.VerifyTransaction(&tx) {
					txs = append(txs, &tx)
				}
			}

			if len(txs) == 0 {
				fmt.Println("All transactions are invalid! Waiting for new ones...")
				return
			}

			//验证后的交易被放到一个块里，同时还有附带奖励的 coinbase 交易。当块被挖出来以后，UTXO 集会被重新索引。
			cbTx := NewCoinbaseTX(miningAddress, "")
			txs = append(txs, cbTx)

			newBlock := bc.MineBlock(txs)
			UTXOSet := UTXOSet{bc}
			//UTXOSet.Reindex()
			UTXOSet.Update(newBlock)

			fmt.Println("New block is mined!")

			//当一笔交易被挖出来以后，就会被从内存池中移除。
			//当前节点所连接到的所有其他节点，接收带有新块哈希的 inv 消息。
			//在处理完消息后，它们可以对块进行请求
			for _, tx := range txs {
				txID := hex.EncodeToString(tx.ID)
				delete(mempool, txID)
			}

			for _, node := range knownNodes {
				if node != nodeAddress {
					sendInv(node, "block", [][]byte{newBlock.Hash})
				}
			}

			if len(mempool) > 0 {
				goto MineTransactions
			}
		}
	}
}

func handleVersion(request []byte, bc *Blockchain) {
	var buff bytes.Buffer
	var payload verzion

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	//节点将从消息中提取的 BestHeight 与自身进行比较。如果自身节点的区块链更长，它会回复 version 消息；否则，它会发送 getblocks 消息
	myBestHeight := bc.GetBestHeight()
	foreignerBestHeight := payload.BestHeight

	if myBestHeight < foreignerBestHeight {
		sendGetBlocks(payload.AddrFrom)
	} else if myBestHeight > foreignerBestHeight {
		sendVersion(payload.AddrFrom, bc)
	}

	if !nodeIsKnown(payload.AddrFrom) {
		knownNodes = append(knownNodes, payload.AddrFrom)
	}
}

//对不同指令的操作
func handleConnection(conn net.Conn, bc *Blockchain) {
	request, err := ioutil.ReadAll(conn)
	if err != nil {
		log.Panic(err)
	}
	command := bytesToCommand(request[:commandLength])
	fmt.Printf("Received %s command\n", command)

	switch command {
	case "addr":
		handleAddr(request)
	case "block":
		handleBlock(request, bc)
	case "inv":
		handleInv(request, bc)
	case "getblocks":
		handleGetBlocks(request, bc)
	case "getdata":
		handleGetData(request, bc)
	case "tx":
		handleTx(request, bc)
	case "version":
		handleVersion(request, bc)
	default:
		fmt.Println("Unknown command!")
	}

	conn.Close()
}

//打开服务器
func StartServer(nodeID, minerAddress string) {
	nodeAddress = fmt.Sprintf("localhost:%s", nodeID)
	miningAddress = minerAddress
	ln, err := net.Listen(protocol, nodeAddress)
	if err != nil {
		log.Panic(err)
	}
	defer ln.Close()

	bc := NewBlockchain(nodeID)

	if nodeAddress != knownNodes[0] {
		sendVersion(knownNodes[0], bc)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Panic(err)
		}
		go handleConnection(conn, bc)
	}
}

//将数据进行编码
func gobEncode(data interface{}) []byte {
	var buff bytes.Buffer

	enc := gob.NewEncoder(&buff)
	err := enc.Encode(data)
	if err != nil {
		log.Panic(err)
	}

	return buff.Bytes()
}

//检测节点是否已知
func nodeIsKnown(addr string) bool {
	for _, node := range knownNodes {
		if node == addr {
			return true
		}
	}

	return false
}
