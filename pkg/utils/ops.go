package utils

import (
	"context"
	"fmt"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"io"
	"k8s.io/apimachinery/pkg/util/wait"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultCheckIpPortInterval = 5
)

type SshClient struct {
	Username    string
	Password    string
	PrivateKey  []byte
	Ip          string
	Port        int
	HostDevices []string
	Client      *ssh.Client
}

func (s *SshClient) Close() error {
	_ = s.Client.Close()
	return nil
}

func (s *SshClient) Operation(ctx context.Context, cmd string) ([]byte, error) {
	session, err := s.Client.NewSession()
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	// 超时控制
	stopChan := make(chan struct{}, 1)
	defer close(stopChan)

	errChan := make(chan error, 1)
	defer close(errChan)

	var res []byte
	go func() {
		defer func() {
			if err := recover(); err != nil {
				zap.L().Error("ssh client operation", zap.Any("err", err))
			}
		}()
		res, err = session.CombinedOutput(cmd)
		if err != nil {
			errChan <- err
			return
		}
		// 有些时候命令执行失败也不会有err,需要根据输出结果判断失败.
		// 如 普通用户执行"fdisk -l" 会提示 "Permission denied",但返回码还是0
		// 但是 普通用户执行"ls /root" 会提示“/root': Permission denied”，且返回码是2.
		// 没有普遍规律。需要调用方自己处理
		// https://www.redhat.com/sysadmin/exit-codes-demystified
		stopChan <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		zap.L().Error("ctx timeout")
		return nil, fmt.Errorf("ctx timeout")
	case <-stopChan:
		zap.L().Debug("operation end")
	case e := <-errChan:
		zap.L().Error("operation err", zap.Error(e), zap.String("res", string(res)))
		return res, e
	}

	if err != nil {
		return res, err
	}
	return res, nil
}

func (s *SshClient) DownloadFile(ctx context.Context, downUrl, dstPath string) error {
	//cmd := fmt.Sprintf("curl -v -o %s -L '%s' ", filepath.Join(ScanToolPath, types.CosFileVulnScanTool), downUrl)
	cmd := fmt.Sprintf("sudo curl -v -o %s -L '%s' ", dstPath, downUrl)
	res, err := s.Operation(ctx, cmd)
	if err != nil {
		zap.L().Error("failed to down vuln scan tool.", zap.String("cmd", cmd), zap.String("output", string(res)), zap.Error(err))
		return err
	}
	zap.L().Debug("download file ok", zap.String("cmd", cmd))
	return nil
}

func (s *SshClient) ChmodFile(ctx context.Context, dstFile string) error {
	cmd := fmt.Sprintf("sudo chmod +x %s", dstFile)
	res, err := s.Operation(ctx, cmd)
	if err != nil {
		zap.L().Error("failed to chmod file.", zap.String("output", string(res)), zap.Error(err))
		return err
	}
	return nil
}

func (s *SshClient) FileExist(ctx context.Context, dstFile string) bool {
	cmd := fmt.Sprintf(`
		if sudo test -e %s; then
			echo "1"
		else
			echo "0"
		fi
	`, dstFile)
	//cmd := fmt.Sprintf("sudo ls %s", dstFile)
	//cmd := fmt.Sprintf("sudo bash -c %s", dstFile)
	res, err := s.Operation(ctx, cmd)
	if err != nil {
		zap.L().Debug("file not exist", zap.String("file", dstFile), zap.String("out", string(res)))
		return false
	}
	return strings.TrimSuffix(string(res), "\n") == "1"
	//return true
}

// DownloadHostFile 通过sftp下载主机文件
func (s *SshClient) DownloadHostFile(ctx context.Context, hostFile string) (string, error) {
	sftpClient, err := sftp.NewClient(s.Client)
	if err != nil {
		zap.L().Error("failed to create sftp client", zap.Error(err))
		return "", err
	}
	defer func() {
		_ = sftpClient.Close()
		zap.L().Debug("sftpclient closed")
	}()

	doneChan := make(chan struct{}, 1)
	errChan := make(chan error, 1)
	localFile := fmt.Sprintf("/tmp/%s", filepath.Base(hostFile))
	go func() {
		f, err := sftpClient.Open(hostFile)
		if err != nil {
			zap.L().Error("failed to open host file", zap.Error(err), zap.String("hostFile", hostFile))
			errChan <- err
			return
		}
		defer func() { _ = f.Close() }()

		l, err := os.Create(localFile)
		if err != nil {
			zap.L().Error("failed to create local file", zap.Error(err), zap.String("localFile", localFile))
			errChan <- err
			return
		}
		defer func() { _ = l.Close() }()

		_, err = io.Copy(l, f)
		if err != nil {
			zap.L().Error("failed to copy remote file to local file", zap.Error(err))
			errChan <- err
			return
		}
		doneChan <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timeout")
	case <-doneChan:
		return localFile, nil
	case retErr := <-errChan:
		return "", retErr
	}
}

// GetBlockDevices 获取主机上的所有块设备
func (s *SshClient) GetBlockDevices(ctx context.Context) ([]string, error) {
	cmd := fmt.Sprintf("sudo fdisk -l | grep -o '^Disk /dev/[^:]*' |awk '{print $2}'")
	data, err := s.Operation(ctx, cmd)
	if err != nil {
		return nil, err
	}

	res := make([]string, 0)
	devs := strings.Split(string(data), "\n")
	for _, v := range devs {
		if len(v) == 0 {
			continue
		}
		res = append(res, v)
	}
	return res, nil

}

// GetBlockDeviceUUID 获取设备的uuid.
// lsblk -l -o NAME,UUID | grep -w xvdb1 | awk '{print $2}'
// 08244b87-a72e-44b0-9536-c2b6010094e0
func (s *SshClient) GetBlockDeviceUUID(ctx context.Context, devName string) (string, error) {
	if strings.HasPrefix(devName, "/dev/") {
		// 一点兼容工作. lsblk 结果里面不包括 "/dev/"
		devName = strings.TrimPrefix(devName, "/dev/")
	}

	cmd := fmt.Sprintf("sudo lsblk -l -o NAME,UUID | grep -w %s | awk '{print $2}'", devName)
	data, err := s.Operation(ctx, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// MountDev mount dev到某个目录
// @param option mount 的选项
func (s *SshClient) MountDev(ctx context.Context, devName, dirName, option string) ([]byte, error) {
	cmd := fmt.Sprintf("sudo mount  %s  %s %s", option, devName, dirName)
	return s.Operation(ctx, cmd)
}

func (s *SshClient) MkDir(ctx context.Context, dirName string) error {
	cmd := fmt.Sprintf("sudo sh -c '[ -d \"%s\" ] || mkdir \"%s\" '", dirName, dirName)
	_, err := s.Operation(ctx, cmd)
	return err
}

// BlockDevFsType 获取设备的文件系统。
func (s *SshClient) BlockDevFsType(ctx context.Context, devName string) (string, error) {
	if strings.HasPrefix(devName, "/dev/") {
		devName = strings.TrimPrefix(devName, "/dev/")
	}
	cmd := fmt.Sprintf("sudo  lsblk -l -o NAME,FSTYPE | grep -w %s | awk '{print $2}'", devName)
	res, err := s.Operation(ctx, cmd)
	return strings.TrimSuffix(string(res), "\n"), err
}

// GetIndeedDev for: /dev/xvda -> nvme0n1,return /dev/nvme0n1; for: /dev/nvme0n1,return itself
func (s *SshClient) GetIndeedDev(ctx context.Context, devName string) (string, error) {
	cmd := fmt.Sprintf("sudo readlink %s", devName)
	res, err := s.Operation(ctx, cmd)
	if err != nil {
		return "", err
	}
	tmpDev := strings.TrimSuffix(string(res), "\n")
	if len(tmpDev) == 0 {
		// not system link,return itself
		return devName, nil
	}
	return filepath.Join("/dev", tmpDev), nil
}

func NewSshClient(username, password, ip string, port int) (*SshClient, error) {
	s := &SshClient{
		Username: username,
		Password: password,
		Ip:       ip,
		Port:     port,
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意：这里忽略了主机密钥验证，请谨慎使用
		Timeout:         100 * time.Second,
	}
	zap.L().Debug("ssh conn config", zap.String("user", username), zap.String("ip", ip), zap.Int("port", port))

	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), config)
	if err != nil {
		zap.L().Error("failed to connect to host.", zap.Error(err))
		return nil, err
	}
	s.Client = conn

	return s, nil
}

func NewSshClientWithRsaKey(username string, privateKey []byte, ip string, port int) (*SshClient, error) {
	s := &SshClient{
		PrivateKey: privateKey,
		Ip:         ip,
		Port:       port,
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), config)
	if err != nil {
		zap.L().Error("failed to connect to host.", zap.Error(err))
		return nil, err
	}
	s.Client = conn
	return s, nil
}

func WaitIpPortAvailable(ctx context.Context, ip string, port int) bool {

	address := fmt.Sprintf("%s:%d", ip, port)
	err := wait.PollUntilContextCancel(ctx, DefaultCheckIpPortInterval*time.Second, false, func(ctx context.Context) (done bool, err error) {
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err != nil {
			zap.L().Warn("host network not ready yet,wait", zap.String("ip", ip), zap.Int("port", port))
			return false, nil
		}
		defer func() { _ = conn.Close() }()
		zap.L().Debug("host network ready", zap.String("ip", ip), zap.Int("port", port))

		return true, nil
	})
	if err != nil {
		return false
	}
	return true
}
