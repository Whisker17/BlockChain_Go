package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"
)

//挖出新块的奖励
const subsidy = 10

//对于每一笔交易来说，它的输入都会引用之前一笔交易的输出（除了最开始的Coinbase）
//即，将之前一笔交易的输出作为本交易的输入
type Transaction struct {
	ID   []byte
	Vin  []TXInput
	Vout []TXOutput
}

//判断是否是Coinbase交易
//判断方法：tx.Vin长度为1且tx.Vin[0].Txid为0tx.Vin[0].Vout为-1
func (tx Transaction) IsCoinbase() bool {
	return len(tx.Vin) == 1 && len(tx.Vin[0].Txid) == 0 && tx.Vin[0].Vout == -1
}

//序列化，根据地址随机生成一个
func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer

	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}

	return encoded.Bytes()
}

//生成一个交易的Hash
func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	txCopy.ID = []byte{}

	hash = sha256.Sum256(txCopy.Serialize())

	return hash[:]
}

//交易必须被签名，因为这是比特币里面保证发送方不会花费属于其他人的币的唯一方式
//如果一个签名是无效的，那么这笔交易就会被认为是无效的，因此，这笔交易也就无法被加到区块链中
//如果仅仅对输出值进行签名并没有什么意义，因为签名不会考虑发送方和接收方
/*
考虑到交易解锁的是之前的输出，然后重新分配里面的价值，并锁定新的输出，那么必须要签名以下数据：
1.存储在已解锁输出的公钥哈希。它识别了一笔交易的“发送方”
2.存储在新的锁定输出里面的公钥哈希。它识别了一笔交易的“接收方”
3.新的输出值
*/
func (tx *Transaction) Sign(privKey ecdsa.PrivateKey, prevTXs map[string]Transaction) {
	//coinbase 交易因为没有实际输入，所以没有被签名
	if tx.IsCoinbase() {
		return
	}

	for _, vin := range tx.Vin {
		if prevTXs[hex.EncodeToString(vin.Txid)].ID == nil {
			log.Panic("ERROR: Previous transaction is not correct")
		}
	}

	//将会被签署的是修剪后的交易副本，而不是一个完整交易
	txCopy := tx.TrimmedCopy()

	//
	for inID, vin := range txCopy.Vin {
		//迭代prevTXs中的交易
		prevTx := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		//Hash 方法对交易进行序列化，并使用 SHA-256 算法进行哈希。哈希后的结果就是我们要签名的数据。
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash

		dataToSign := fmt.Sprintf("%x\n", txCopy)

		//我们通过 privKey 对 txCopy.ID 进行签名。
		//一个 ECDSA 签名就是一对数字，我们对这对数字连接起来，并存储在输入的 Signature 字段。
		r, s, err := ecdsa.Sign(rand.Reader, &privKey, []byte(dataToSign))
		if err != nil {
			log.Panic(err)
		}
		signature := append(r.Bytes(), s.Bytes()...)

		//在获取完哈希，我们应该重置 PubKey 字段，以便于它不会影响后面的迭代。
		tx.Vin[inID].Signature = signature
		txCopy.Vin[inID].PubKey = nil
	}
}

func (tx Transaction) String() string {
	var lines []string
	lines = append(lines, fmt.Sprintf("--- Transaction %x:", tx.ID))

	for i, input := range tx.Vin {
		lines = append(lines, fmt.Sprintf("     Input %d:", i))
		lines = append(lines, fmt.Sprintf("       TXID:      %x", input.Txid))
		lines = append(lines, fmt.Sprintf("       Out:       %d", input.Vout))
		lines = append(lines, fmt.Sprintf("       Signature: %x", input.Signature))
		lines = append(lines, fmt.Sprintf("       PubKey:    %x", input.PubKey))
	}

	for i, output := range tx.Vout {
		lines = append(lines, fmt.Sprintf("     Output %d:", i))
		lines = append(lines, fmt.Sprintf("       Value:  %d", output.Value))
		lines = append(lines, fmt.Sprintf("       Script: %x", output.PubKeyHash))
	}

	return strings.Join(lines, "\n")
}

//这个副本包含了所有的输入和输出，但是 TXInput.Signature 和 TXIput.PubKey 被设置为 nil
//因为Signature和PubKey需要在签名时被重置
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

//验证交易
func (tx *Transaction) Verify(prevTXs map[string]Transaction) bool {
	if tx.IsCoinbase() {
		return true
	}

	for _, vin := range tx.Vin {
		if prevTXs[hex.EncodeToString(vin.Txid)].ID == nil {
			log.Panic("ERROR: Previous transaction is not correct")
		}
	}

	//同一笔交易的副本
	txCopy := tx.TrimmedCopy()
	//相同的区块用于生成密钥对
	curve := elliptic.P256()

	for inID, vin := range tx.Vin {
		prevTX := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		txCopy.Vin[inID].PubKey = prevTX.Vout[vin.Vout].PubKeyHash

		////解包存储在 TXInput.Signature 和 TXInput.PubKey 中的值
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

		dataToVerify := fmt.Sprintf("%x\n", txCopy)

		//使用从输入提取的公钥创建了一个 ecdsa.PublicKey，通过传入输入中提取的签名执行了 ecdsa.Verify
		rawPubKey := ecdsa.PublicKey{curve, &x, &y}
		if ecdsa.Verify(&rawPubKey, []byte(dataToVerify), &r, &s) == false {
			return false
		}
		txCopy.Vin[inID].PubKey = nil
	}

	return true
}

//当矿工挖出一个新的块时，会向新的块中添加一个coinbase交易
//coinbase交易不需要引用之前一笔交易的输出
func NewCoinbaseTX(to, data string) *Transaction {
	if data == "" {
		randData := make([]byte, 20)
		_, err := rand.Read(randData)
		if err != nil {
			log.Panic(err)
		}

		data = fmt.Sprintf("%x", randData)
	}

	txin := TXInput{[]byte{}, -1, nil, []byte(data)}
	txout := NewTXOutput(subsidy, to)
	tx := Transaction{nil, []TXInput{txin}, []TXOutput{*txout}}
	tx.ID = tx.Hash()

	return &tx
}

//新建一个UTXO交易
func NewUTXOTransaction(wallet *Wallet, to string, amount int, UTXOSet *UTXOSet) *Transaction {
	var inputs []TXInput
	var outputs []TXOutput

	//对公钥加密（一次sha256，一次RIPEMD-160）
	pubKeyHash := HashPubKey(wallet.PublicKey)
	//在UTXO集中找到满足此公钥的UTXO
	acc, validOutputs := UTXOSet.FindSpendableOutput(pubKeyHash, amount)

	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}

	//遍历UTXO集中选出的UTXO
	for txid, outs := range validOutputs {
		//将string类型转换成[]byte
		txID, err := hex.DecodeString(txid)
		if err != nil {
			log.Panic(err)
		}

		//遍历UTXO集中选出的UTXO，并借此生成TXInput
		for _, out := range outs {
			input := TXInput{txID, out, nil, wallet.PublicKey}
			inputs = append(inputs, input)
		}
	}

	from := fmt.Sprintf("%s", wallet.GetAddress())
	outputs = append(outputs, *NewTXOutput(amount, to))
	//如果选出的UTXO中余额总值大于所需，则多生成一个新的TXOutput
	if acc > amount {
		outputs = append(outputs, *NewTXOutput(acc-amount, from))
	}

	//生成交易
	tx := Transaction{nil, inputs, outputs}
	tx.ID = tx.Hash()
	//对该新生成的交易进行数字签名
	UTXOSet.Blockchain.SignTransaction(&tx, wallet.PrivateKey)

	return &tx
}

//将[]byte类型转换成Transaction
func DeserializeTransaction(data []byte) Transaction {
	var transaction Transaction

	decoder := gob.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(&transaction)
	if err != nil {
		log.Panic(err)
	}

	return transaction
}
