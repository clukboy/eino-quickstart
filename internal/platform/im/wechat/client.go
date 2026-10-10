package wechat

import (
	"errors"
	"sync"

	"github.com/ArtisanCloud/PowerWeChat/v3/src/kernel"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/miniProgram"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/officialAccount"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/work"
)

type Client struct {
	program  map[string]*miniProgram.MiniProgram
	official map[string]*officialAccount.OfficialAccount
	work     map[string]*work.Work
	cfg      map[string]*Config
	cache kernel.CacheInterface
	mu sync.Mutex
}
type Config struct {
	AppID     string `yaml:"appId"`
	Secret    string `yaml:"secret"`
	AgentID   int    `yaml:"agentId"`
	Token     string `yaml:"token"`
	AESKey    string `yaml:"aesKey"`
	HttpDebug bool   `yaml:"httpDebug"`
	
}

func NewClient(config map[string]*Config, cacheCfg *kernel.UniversalOptions) *Client {

	return &Client{
		program:  make(map[string]*miniProgram.MiniProgram),
		official: make(map[string]*officialAccount.OfficialAccount),
		work:     make(map[string]*work.Work),
		cfg:      config,
		cache: kernel.NewInteractsWithCache(kernel.NewRedisClient(cacheCfg)).GetCache(),
	}

}

func (c *Client) GetMiniProgram(serviceName string) (*miniProgram.MiniProgram, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mini, ok := c.program[serviceName]
	if !ok {
		cfg, cfgOk := c.cfg[serviceName]
		if !cfgOk {
			return nil, errors.New("config not found")
		}
		mini, _ = miniProgram.NewMiniProgram(&miniProgram.UserConfig{
			AppID:     cfg.AppID,
			Secret:    cfg.Secret,
			HttpDebug: cfg.HttpDebug,
			Cache:     c.cache,
		})
		c.program[serviceName] = mini
	}
	return mini, nil
}

func (c *Client) GetOfficialAccount(serviceName string) (*officialAccount.OfficialAccount, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	official, ok := c.official[serviceName]
	if !ok {
		cfg, cfgOk := c.cfg[serviceName]
		if !cfgOk {
			return nil, errors.New("config not found")
		}
		official, _ = officialAccount.NewOfficialAccount(&officialAccount.UserConfig{
			AppID:     cfg.AppID,
			Secret:    cfg.Secret,
			HttpDebug: cfg.HttpDebug,
			Cache:     c.cache,
			Token:     cfg.Token,
			AESKey:    cfg.AESKey,
		})
		c.official[serviceName] = official
	}
	return official, nil
}
func (c *Client) GetWork(serviceName string) (*work.Work, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	weWork, ok := c.work[serviceName]
	if !ok {
		cfg, cfgOk := c.cfg[serviceName]
		if !cfgOk {
			return nil, errors.New("config not found")
		}
		weWork, _ = work.NewWork(&work.UserConfig{
			CorpID:    cfg.AppID,
			Secret:    cfg.Secret,
			AgentID:   cfg.AgentID,
			HttpDebug: cfg.HttpDebug,
			Cache:     c.cache,
			OAuth: work.OAuth{
				Callback: "https://www.baidu.com",
				Scopes:   nil,
			},
			Token:  cfg.Token,
			AESKey: cfg.AESKey,
		})
		c.work[serviceName] = weWork
	}
	return weWork, nil
}
