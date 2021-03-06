package gate

import (
	"net"
	"sync"

	"github.com/juju/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dearcode/candy/server/meta"
	"github.com/dearcode/candy/server/util/log"
)

var (
	// ErrUndefineMethod 方法未定义.
	ErrUndefineMethod = errors.New("undefine method")
	// ErrInvalidContext 从context中解析客户端地址时出错.
	ErrInvalidContext = errors.New("invalid context")
	// ErrInvalidState 当前用户离线或未登录.
	ErrInvalidState = errors.New("invalid context")
)

// Gate recv client request.
type Gate struct {
	host     string
	master   *master
	store    *store
	sessions map[string]*session
	ids      map[int64]*session
	sync.RWMutex
}

// NewGate new gate server.
func NewGate(host, master, store string) *Gate {
	return &Gate{
		host:     host,
		master:   newMaster(master),
		store:    newStore(store),
		sessions: make(map[string]*session),
		ids:      make(map[int64]*session),
	}
}

// Start Gate service.
func (g *Gate) Start() error {
	log.Debugf("Gate Start...")
	serv := grpc.NewServer()
	meta.RegisterGateServer(serv, g)

	lis, err := net.Listen("tcp", g.host)
	if err != nil {
		return errors.Trace(err)
	}

	if err = g.master.start(); err != nil {
		return errors.Trace(err)
	}

	if err = g.store.start(); err != nil {
		return errors.Trace(err)
	}

	return serv.Serve(lis)
}

func (g *Gate) getSession(ctx context.Context) (*session, error) {
	log.Debug("Gate getSession")
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return nil, errors.Trace(ErrInvalidContext)
	}

	addrs, ok := md["remote"]
	if !ok {
		return nil, errors.Trace(ErrInvalidContext)
	}

	g.RLock()
	s, ok := g.sessions[addrs[0]]
	g.RUnlock()

	if !ok {
		s = newSession(addrs[0])
		g.Lock()
		g.sessions[addrs[0]] = s
		g.Unlock()
	}

	return s, nil
}

func (g *Gate) addOnlineUser(s *session) {
	g.Lock()
	g.ids[s.id] = s
	g.Unlock()
}

func (g *Gate) getOnlineUser(id int64) *session {
	g.RLock()
	s, ok := g.ids[id]
	g.RUnlock()

	if ok {
		return s
	}

	return nil
}

func (g *Gate) delOnlineUser(s *session) {
	g.Lock()
	delete(g.ids, s.id)
	g.Unlock()
}

func (g *Gate) getOnlineSession(ctx context.Context) (*session, error) {
	log.Debug("Gate getOnlineSession")
	s, err := g.getSession(ctx)
	if err != nil {
		return nil, errors.Trace(err)
	}

	if !s.isOnline() {
		return nil, ErrInvalidState
	}

	return s, nil
}

// Register user, passwd.
func (g *Gate) Register(ctx context.Context, req *meta.GateRegisterRequest) (*meta.GateRegisterResponse, error) {
	log.Debug("Gate Register")
	_, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateRegisterResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	id, err := g.master.newID()
	if err != nil {
		return &meta.GateRegisterResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("Register user:%v password:%v", req.User, req.Password)

	if err = g.store.register(req.User, req.Password, id); err != nil {
		return &meta.GateRegisterResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateRegisterResponse{ID: id}, nil
}

// UpdateUserInfo nickname.
func (g *Gate) UpdateUserInfo(ctx context.Context, req *meta.GateUpdateUserInfoRequest) (*meta.GateUpdateUserInfoResponse, error) {
	log.Debug("Gate UpdateUserInfo")
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateUpdateUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	if !s.isOnline() {
		err := errors.Errorf("current user is offline")
		return &meta.GateUpdateUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("updateUserInfo user:%v niceName:%v", req.User, req.NickName)
	id, err := g.store.updateUserInfo(req.User, req.NickName, req.Avatar)
	if err != nil {
		return &meta.GateUpdateUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}, ID: id}, nil
	}

	return &meta.GateUpdateUserInfoResponse{ID: id}, nil
}

// UpdateUserPassword update user password
func (g *Gate) UpdateUserPassword(ctx context.Context, req *meta.GateUpdateUserPasswordRequest) (*meta.GateUpdateUserPasswordResponse, error) {
	log.Debug("Gate UpdateUserPassword")
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateUpdateUserPasswordResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	if !s.isOnline() {
		err := errors.Errorf("current user is offline")
		return &meta.GateUpdateUserPasswordResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("updateUserPassword user:%v", req.User)
	id, err := g.store.updateUserPassword(req.User, req.Password)
	if err != nil {
		return &meta.GateUpdateUserPasswordResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateUpdateUserPasswordResponse{ID: id}, nil
}

// GetUserInfo get user base info
func (g *Gate) GetUserInfo(ctx context.Context, req *meta.GateGetUserInfoRequest) (*meta.GateGetUserInfoResponse, error) {
	log.Debugf("Gate UserInfo")
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateGetUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	if !s.isOnline() {
		err := errors.Errorf("current user is offline")
		return &meta.GateGetUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("get UserInfo user:%v", req.User)
	id, name, nickName, avatar, err := g.store.getUserInfo(req.User)
	if err != nil {
		return &meta.GateGetUserInfoResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateGetUserInfoResponse{ID: id, User: name, NickName: nickName, Avatar: avatar}, nil
}

// Login user,passwd.
func (g *Gate) Login(ctx context.Context, req *meta.GateUserLoginRequest) (*meta.GateUserLoginResponse, error) {
	log.Debug("Gate Login")
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateUserLoginResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("Login user:%v password:%v", req.User, req.Password)
	id, err := g.store.auth(req.User, req.Password)
	if err != nil {
		return &meta.GateUserLoginResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	//登陆成功后订阅消息
	err = g.store.subscribe(id, g.host)
	if err != nil {
		return &meta.GateUserLoginResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	s.online(id)
	g.addOnlineUser(s)

	return &meta.GateUserLoginResponse{ID: id}, nil
}

// Logout nil.
func (g *Gate) Logout(ctx context.Context, req *meta.GateUserLogoutRequest) (*meta.GateUserLogoutResponse, error) {
	log.Debug("Gate Logout")
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateUserLogoutResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("Logout user:%v", req.User)

	//注销需要先取消消息订阅
	err = g.store.unSubscribe(s.getID(), g.host)
	if err != nil {
		return &meta.GateUserLogoutResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	g.delOnlineUser(s)
	s.offline()
	return &meta.GateUserLogoutResponse{}, nil
}

// MessageStream recv user message, send to client.
func (g *Gate) MessageStream(stream meta.Gate_MessageStreamServer) error {
	s, err := g.getSession(stream.Context())
	if err != nil {
		return err
	}
	s.addStream(stream)

	for {
		msg, err := stream.Recv()
		if err != nil {
			log.Errorf("stream Recv error:%s", errors.ErrorStack(err))
			break
		}

		err = g.store.newMessage(msg)
		if err != nil {
			log.Errorf("%v", err)
			continue
		}
	}

	return nil
}

// Heartbeat nil.
func (g *Gate) Heartbeat(ctx context.Context, req *meta.GateHeartbeatRequest) (*meta.GateHeartbeatResponse, error) {
	s, err := g.getSession(ctx)
	if err != nil {
		return &meta.GateHeartbeatResponse{}, nil
	}

	//已经离线就不在处理
	if !s.isOnline() {
		return &meta.GateHeartbeatResponse{}, nil
	}

	//更新心跳信息
	s.heartbeat()

	return &meta.GateHeartbeatResponse{}, nil
}

// Notice recv Notice server Message, and send Message to client.
func (g *Gate) Notice(ctx context.Context, req *meta.GateNoticeRequest) (*meta.GateNoticeResponse, error) {
	log.Debugf("ChannelID:%v msg:%v", req.ChannelID, req.Msg)

	s := g.getOnlineUser(req.ChannelID)
	if s == nil {
		log.Debugf("User:%d offline", req.ChannelID)
		return &meta.GateNoticeResponse{}, nil
	}

	client := s.getStream()
	if client == nil {
		log.Errorf("User:%d client strem is nil", req.ChannelID)
		return &meta.GateNoticeResponse{}, nil
	}

	if err := client.Send(req.Msg); err != nil {
		return &meta.GateNoticeResponse{Header: &meta.ResponseHeader{Code: -1, Msg: errors.ErrorStack(err)}}, nil
	}

	return &meta.GateNoticeResponse{}, nil
}

// AddFriend 添加好友或确认接受添加.
func (g *Gate) AddFriend(ctx context.Context, req *meta.GateAddFriendRequest) (*meta.GateAddFriendResponse, error) {
	s, err := g.getOnlineSession(ctx)
	if err != nil {
		return &meta.GateAddFriendResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	//自己要把自己添加成好友
	if req.UserID == s.id {
		log.Infof("%d add friend id:%d", s.id, req.UserID)
		return &meta.GateAddFriendResponse{Header: &meta.ResponseHeader{Code: -1, Msg: "Friend ID must not be Self ID"}}, nil
	}

	// 主动添加对方为好友，更新自己本地信息
	state, err := g.store.addFriend(s.id, req.UserID, meta.FriendRelation_Active)
	if err != nil {
		log.Errorf("%d addFriend:%d erorr:%s", s.id, req.UserID, errors.ErrorStack(err))
		return &meta.GateAddFriendResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}
	// 如果本地返回 FriendRelation_Confirm 说明之前对方添加过自己
	if state == meta.FriendRelation_Confirm {
		return &meta.GateAddFriendResponse{Confirm: true}, nil
	}

	// 被动添加好友，更新对方好友信息
	if state, err = g.store.addFriend(req.UserID, s.id, meta.FriendRelation_Passive); err != nil {
		log.Errorf("%d addFriend:%d erorr:%s", s.id, req.UserID, errors.ErrorStack(err))
		return &meta.GateAddFriendResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	// 如果对方返回FriendRelation_Confirm，说明之前对方添加过自己, 只不过本地信息未更新，要先更新本地信息，再返回FriendRelation_Confirm
	if state == meta.FriendRelation_Confirm {
		// 正常流程走不到这里，除非是数据丢失了
		_, err := g.store.addFriend(s.id, req.UserID, meta.FriendRelation_Confirm)
		if err != nil {
			log.Errorf("%d addFriend:%d erorr:%s", s.id, req.UserID, errors.ErrorStack(err))
			return &meta.GateAddFriendResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
		}
		return &meta.GateAddFriendResponse{Confirm: true}, nil
	}

	return &meta.GateAddFriendResponse{}, nil
}

// FindUser 添加好友前先查找,模糊查找
func (g *Gate) FindUser(ctx context.Context, req *meta.GateFindUserRequest) (*meta.GateFindUserResponse, error) {
	_, err := g.getOnlineSession(ctx)
	if err != nil {
		log.Errorf("getSession error:%s", errors.ErrorStack(err))
		return &meta.GateFindUserResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}
	users, err := g.store.findUser(req.User)
	if err != nil {
		return &meta.GateFindUserResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}
	return &meta.GateFindUserResponse{Users: users}, nil
}

// CreateGroup 用户创建一个聊天组.
func (g *Gate) CreateGroup(ctx context.Context, req *meta.GateCreateGroupRequest) (*meta.GateCreateGroupResponse, error) {
	s, err := g.getOnlineSession(ctx)
	if err != nil {
		log.Errorf("getSession error:%s", errors.ErrorStack(err))
		return &meta.GateCreateGroupResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}
	gid, err := g.master.newID()
	if err != nil {
		return &meta.GateCreateGroupResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	if err = g.store.createGroup(s.getID(), gid); err != nil {
		return &meta.GateCreateGroupResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	log.Debugf("user:%d, create group:%d", s.getID(), gid)
	return &meta.GateCreateGroupResponse{ID: gid}, nil
}

// UploadFile 客户端上传文件接口，一次一个文件.
func (g *Gate) UploadFile(ctx context.Context, req *meta.GateUploadFileRequest) (*meta.GateUploadFileResponse, error) {
	s, err := g.getOnlineSession(ctx)
	if err != nil {
		log.Errorf("getSession error:%s", errors.ErrorStack(err))
		return &meta.GateUploadFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	if err = g.store.uploadFile(s.id, req.File); err != nil {
		return &meta.GateUploadFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateUploadFileResponse{}, nil
}

// CheckFile 客户端检测文件是否存在，文件的临时ID和md5, 服务器返回不存在的文件ID.
func (g *Gate) CheckFile(ctx context.Context, req *meta.GateCheckFileRequest) (*meta.GateCheckFileResponse, error) {
	s, err := g.getOnlineSession(ctx)
	if err != nil {
		log.Errorf("getSession error:%s", errors.ErrorStack(err))
		return &meta.GateCheckFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	names, err := g.store.checkFile(s.id, req.Names)
	if err != nil {
		return &meta.GateCheckFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateCheckFileResponse{Names: names}, nil
}

// DownloadFile 客户端下载文件，传入ID，返回具体文件内容.
func (g *Gate) DownloadFile(ctx context.Context, req *meta.GateDownloadFileRequest) (*meta.GateDownloadFileResponse, error) {
	s, err := g.getOnlineSession(ctx)
	if err != nil {
		log.Errorf("getSession error:%s", errors.ErrorStack(err))
		return &meta.GateDownloadFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	files, err := g.store.downloadFile(s.id, req.Names)
	if err != nil {
		return &meta.GateDownloadFileResponse{Header: &meta.ResponseHeader{Code: -1, Msg: err.Error()}}, nil
	}

	return &meta.GateDownloadFileResponse{Files: files}, nil
}
