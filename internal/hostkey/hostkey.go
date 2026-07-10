package hostkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/cracer4869/opensvr/internal/logbus"
)

// LoadOrCreate 加载指定路径的 ed25519 SFTP 主机私钥；缺失则生成并以 PEM 持久化。
// ed25519 更现代、握手更快，作为默认主机密钥之一。
func LoadOrCreate(path string) (ssh.Signer, error) {
	return loadOrCreate(path, func() (any, error) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		return priv, err
	})
}

// LoadOrCreateRSA 加载指定路径的 2048 位 RSA SFTP 主机私钥；缺失则生成并持久化。
// RSA 主机密钥兼容性最广：老旧网络设备(华为/H3C/思科等)的 SSH 客户端常只认
// ssh-rsa / rsa-sha2-* 主机密钥而不支持 ed25519，故与 ed25519 一并提供以兜底。
func LoadOrCreateRSA(path string) (ssh.Signer, error) {
	return loadOrCreate(path, func() (any, error) {
		return rsa.GenerateKey(rand.Reader, 2048)
	})
}

// loadOrCreate 通用加载：文件存在则解析私钥；缺失则用 gen 生成并以 OpenSSH PEM 持久化。
// 持久化保证主机密钥指纹跨重启稳定，设备不会反复弹"主机密钥已变更"告警。
// 文件损坏（截断/篡改）时自愈：备份为 <path>.bad 后重新生成——指纹会变（设备端
// 需重新确认主机密钥），但好过整个程序启动失败。
func loadOrCreate(path string, gen func() (any, error)) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		s, perr := ssh.ParsePrivateKey(b)
		if perr == nil {
			return s, nil
		}
		_ = os.Rename(path, path+".bad")
		logbus.Emit(logbus.Event{Proto: "sftp", Action: "hostkey", OK: false,
			Msg: fmt.Sprintf("主机密钥 %s 损坏(%v)，已备份为 .bad 并重新生成，指纹将变化", path, perr)})
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	priv, err := gen()
	if err != nil {
		return nil, err
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// Fingerprint 返回主机公钥的 SHA256 指纹（形如 "SHA256:xxxx"），供页面展示与设备核对。
func Fingerprint(s ssh.Signer) string {
	return ssh.FingerprintSHA256(s.PublicKey())
}
