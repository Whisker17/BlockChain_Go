package main

import (
	"bytes"
	"encoding/gob"
	"log"
)

type TXOutput struct {
	Value      int
	PubKeyHash []byte
}

//简单的锁定一个账户
//将地址解码，从中提取出公钥哈希并保存在 PubKeyHash 字段
func (out *TXOutput) Lock(address []byte) {
	pubKeyHash := Base58Decode(address)
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	out.PubKeyHash = pubKeyHash
}

//检查是否提供的公钥哈希被用于锁定输出
//这是一个 UsesKey 的辅助函数，并且它们都被用于 FindUnspentTransactions 来形成交易之间的联系。
func (out *TXOutput) IsLockedWithKey(pubKeyHash []byte) bool {
	return bytes.Compare(out.PubKeyHash, pubKeyHash) == 0
}

func NewTXOutput(value int, address string) *TXOutput {
	txo := &TXOutput{value, nil}
	txo.Lock([]byte(address))

	return txo
}

type TXOutputs struct {
	Outputs []TXOutput
}

func (outs TXOutputs) Serialize() []byte {
	var buff bytes.Buffer

	enc := gob.NewEncoder(&buff)
	err := enc.Encode(outs)
	if err != nil {
		log.Panic(err)
	}

	return buff.Bytes()
}

func DeserializeOutputs(data []byte) TXOutputs {
	var outputs TXOutputs

	dec := gob.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&outputs)
	if err != nil {
		log.Panic(err)
	}

	return outputs
}
