package main

import "bytes"

//Txid是之前交易的ID
//Vout存储的是该输出在那笔交易中所有输出的索引
//ScriptSig提供可解锁输出结构中ScriptPubKey字段的数据，现在被分为Signature和PubKey两部分
type TXInput struct {
	Txid      []byte
	Vout      int
	Signature []byte
	PubKey    []byte
}

//检查输入使用了指定密钥来解锁一个输出
func (in *TXInput) UsesKey(pubKeyHash []byte) bool {
	lockingHash := HashPubKey(in.PubKey)

	return bytes.Compare(lockingHash, pubKeyHash) == 0
}
