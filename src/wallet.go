package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"github.com/crypto/ripemd160"
	"log"
)

//比特币的地址生成算法：
//https://www.zhihu.com/question/22399196/answer/201836128
//1.生成随机私钥
//2.椭圆曲线算公钥
//3.计算公钥的SHA-256哈希值（加前缀0x04）
//4.计算RIPEMD-160哈希值
//5.加入地址版本号（比特币主网版本号0x00）
//6.计算Sha-256哈希值（连续两次）
//7.取上一步结果的前4个字节（8位十六进制）
//8.把这4个字节加载第五步的结果后面
//9.用Base58编码变换一下地址
const version = byte(0x00)
const addressChecksumLen = 4

//钱包有私钥和公钥，私钥基于椭圆曲线数字签名算法
type Wallet struct {
	PrivateKey ecdsa.PrivateKey
	PublicKey  []byte
}

//创建钱包
func NewWallet() *Wallet {
	private, public := newKeyPair()
	wallet := Wallet{private, public}

	return &wallet
}

//将一个公钥转换成一个Base58地址需要以下步骤：
//1.使用RIPEMD160(SHA256(PubKey))哈希算法，取公钥并对其哈希两次
//2.给哈希加上地址生成算法版本的前缀
//3.对于第二步生成的结果，使用SHA256(SHA256(payload))再哈希，计算校验和，校验和是结果哈希的前四个字节
//4.将校验和附加到version+PubKeyHash的组合中
//5.使用Base58对version+PubKeyHash+checksum组合进行编码
func (w Wallet) GetAddress() []byte {
	pubKeyHash := HashPubKey(w.PublicKey)

	versionedPayload := append([]byte{version}, pubKeyHash...)
	checksum := checksum(versionedPayload)

	fullPayload := append(versionedPayload, checksum...)
	address := Base58Encode(fullPayload)

	return address
}

//对公钥进行Hash
//先进行一次sha256，然后一次RIPEMD-160
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

func ValidateAddress(address string) bool {
	pubKeyHash := Base58Decode([]byte(address))
	actualChecksum := pubKeyHash[len(pubKeyHash)-addressChecksumLen:]
	version := pubKeyHash[0]
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-addressChecksumLen]
	targetChecksum := checksum(append([]byte{version}, pubKeyHash...))

	return bytes.Compare(actualChecksum, targetChecksum) == 0
}

//双重hash之后的校验和
func checksum(payload []byte) []byte {
	firstSHA := sha256.Sum256(payload)
	secondSHA := sha256.Sum256(firstSHA[:])

	return secondSHA[:addressChecksumLen]
}

//在基于椭圆曲线的算法中，公钥是曲线上的点，公钥是X，Y坐标的组合
func newKeyPair() (ecdsa.PrivateKey, []byte) {
	curve := elliptic.P256()
	private, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		log.Panic(err)
	}
	pubKey := append(private.PublicKey.X.Bytes(), private.PublicKey.Y.Bytes()...)

	return *private, pubKey
}
