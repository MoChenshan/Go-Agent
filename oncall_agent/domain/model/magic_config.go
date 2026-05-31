// Package model 提供领域模型共享类型
package model

import "time"

// OutputDataType 输出类型的数据类型枚举
type OutputDataType int

const (
	OutputDataTypeBool OutputDataType = 1 // Bool type
	OutputDataTypeInt  OutputDataType = 2 // Int type
)

// ActInfo 活动信息表结构
type ActInfo struct {
	ID                     int       `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	Type                   int       `gorm:"column:c_type;type:int(10);not null" json:"type"`
	Name                   string    `gorm:"column:c_name;type:varchar(512);not null" json:"name"`
	Desc                   string    `gorm:"column:c_desc;type:varchar(512);not null" json:"desc"`
	H5URL                  string    `gorm:"column:c_h5_url;type:varchar(512);not null" json:"h5_url"`
	WebURL                 string    `gorm:"column:c_web_url;type:varchar(512);not null" json:"web_url"`
	HeadURL                string    `gorm:"column:c_head_url;type:varchar(512);not null" json:"head_url"`
	BeginTime              time.Time `gorm:"column:c_b_time;type:timestamp;default:'0000-00-00 00:00:00'" json:"begin_time"`
	EndTime                time.Time `gorm:"column:c_e_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"end_time"`
	ModifyTime             time.Time `gorm:"column:c_m_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"modify_time"`
	CreateTime             time.Time `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator               string    `gorm:"column:c_operator;type:varchar(512)" json:"operator"`
	TaskRedisZK            string    `gorm:"column:c_task_redis_zk;type:varchar(64)" json:"task_redis_zk"`
	TaskRedisAuth          string    `gorm:"column:c_task_redis_auth;type:varchar(64)" json:"task_redis_auth"`
	AttentHuman            int       `gorm:"column:c_attent_human;type:int(10)" json:"attent_human"`
	LeftHuman              int       `gorm:"column:c_left_human;type:int(10)" json:"left_human"`
	DisplayType            int       `gorm:"column:c_display_type;type:int(10)" json:"display_type"`
	Tag1                   int       `gorm:"column:c_tag_1;type:int(10)" json:"tag_1"`
	Tag2                   int       `gorm:"column:c_tag_2;type:int(10)" json:"tag_2"`
	Tag3                   int       `gorm:"column:c_tag_3;type:int(10)" json:"tag_3"`
	Tag4                   int       `gorm:"column:c_tag_4;type:int(10)" json:"tag_4"`
	CaiyunID               string    `gorm:"column:c_caiyun_id;type:varchar(32)" json:"caiyun_id"`
	ExtConf                string    `gorm:"column:c_ext_conf;type:mediumtext" json:"ext_conf"`
	Attentors              string    `gorm:"column:c_attentors;type:mediumtext" json:"attentors"`
	WhiteFlag              int       `gorm:"column:c_white_flag;type:int(11)" json:"white_flag"`
	Enable                 int       `gorm:"column:c_enable;type:tinyint(4);default:1" json:"enable"`
	ActTagPos              int       `gorm:"column:act_tag_pos;type:int(10)" json:"act_tag_pos"`
	ActTagType             int       `gorm:"column:act_tag_type;type:int(10)" json:"act_tag_type"`
	PMSID                  string    `gorm:"column:c_pmsid;type:varchar(128)" json:"pmsid"`
	Status                 int       `gorm:"column:c_status;type:int(11);comment:'1: 预发布状态 4: 发布状态'" json:"status"`
	LastPublishTime        time.Time `gorm:"column:c_last_publish_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"last_publish_time"`
	LastModifyTime         time.Time `gorm:"column:c_last_modify_time;type:timestamp;default:CURRENT_TIMESTAMP" json:"last_modify_time"`
	NeedPMS                int       `gorm:"column:c_need_pms;type:tinyint(4);default:1" json:"need_pms"`
	AlarmPMSID             string    `gorm:"column:c_alram_pmsid;type:varchar(128)" json:"alarm_pmsid"`
	AlarmSendTimes         int       `gorm:"column:c_alarm_send_times;type:int(11)" json:"alarm_send_times"`
	LastAlarmSendTime      int64     `gorm:"column:c_last_alram_send_time;type:bigint(20)" json:"last_alarm_send_time"`
	AppID                  int       `gorm:"column:c_app_id;type:int(11)" json:"app_id"`
	LabelIDList            string    `gorm:"column:c_label_id_list;type:tinytext;comment:'标签名 list'" json:"label_id_list"`
	SupportAccountTypeList string    `gorm:"column:c_support_account_type_list;type:varchar(512);comment:'支持的登录类型'" json:"support_account_type_list"`
	PageType               int       `gorm:"column:c_page_type;type:int(11);default:0;comment:'活动类型：0-页面+模块 1-纯模块'" json:"page_type"`
	PageStatus             int       `gorm:"column:c_page_status;type:int(11);default:0;comment:'页面状态：0-修改中 1-已测试部署/预发布 2-已发布'" json:"page_status"`
	CryptoID               string    `gorm:"column:c_crypro_id;type:varchar(128);comment:'加密id'" json:"crypto_id"`
	ActName                string    `gorm:"column:c_act_name;type:varchar(512);default:'';comment:'活动名称'" json:"act_name"`
	CodeOperator           string    `gorm:"column:c_code_operator;type:varchar(512);default:'';comment:'活动code的操作人'" json:"code_operator"`
	Developer              string    `gorm:"column:c_developer;type:varchar(512);not null;default:'';comment:'活动开发者'" json:"developer"`
	WorkflowID             string    `gorm:"column:c_workflow_id;type:varchar(10);not null;default:'';comment:'工作流ID'" json:"workflow_id"`
	PublishNotifyTaskID    string    `gorm:"column:c_publish_notify_task_id;type:varchar(64);comment:'审批后未发布通知任务 ID'" json:"publish_notify_task_id"`
	Permanent              int       `gorm:"column:c_permanent;type:int(11);default:0;comment:'是否是长期活动'" json:"permanent"`
}

// TableName 指定表名
func (ActInfo) TableName() string {
	return "t_act_info"
}

// ModuleInfo 模块信息表结构
type ModuleInfo struct {
	ID              int       `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	Type            int32     `gorm:"column:c_type;type:int(10);not null" json:"type"`
	Name            string    `gorm:"column:c_name;type:varchar(512);not null" json:"name"`
	Desc            string    `gorm:"column:c_desc;type:varchar(512);not null" json:"desc"`
	BeginTime       time.Time `gorm:"column:c_b_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"begin_time"`
	EndTime         time.Time `gorm:"column:c_e_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"end_time"`
	ModifyTime      time.Time `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime      time.Time `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator        string    `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	ActID           int       `gorm:"column:c_act_id;type:int(10)" json:"act_id"`
	ExtConf         string    `gorm:"column:c_ext_conf;type:mediumtext" json:"ext_conf"`
	DataType        int       `gorm:"column:c_data_type;type:int(11)" json:"data_type"`
	IsOtherTime     int       `gorm:"column:c_is_other_time;type:int(11)" json:"is_other_time"`
	Enable          int       `gorm:"column:c_enable;type:tinyint(4);default:1" json:"enable"`
	PushTitle       string    `gorm:"column:c_push_title;type:varchar(128)" json:"push_title"`
	PushDesc        string    `gorm:"column:c_push_desc;type:mediumtext" json:"push_desc"`
	Locker          string    `gorm:"column:locker;type:varchar(128);comment:'当前编辑的人'" json:"locker"`
	LockTime        time.Time `gorm:"column:lock_time;type:datetime;comment:'锁定时间'" json:"lock_time"`
	CryptoID        string    `gorm:"column:c_crypro_id;type:varchar(128);comment:'加密id'" json:"crypto_id"`
	LastPublishTime time.Time `gorm:"column:c_last_publish_time;type:timestamp;not null;default:'0000-00-00 00:00:00';comment:'发布时间'" json:"last_publish_time"`
}

// TableName 指定表名
func (ModuleInfo) TableName() string {
	return "t_module_info"
}

// ModuleType 模块类型表结构
type ModuleType struct {
	ID         int       `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	Type       int       `gorm:"column:c_type;type:int(10);not null;comment:'1-正常使用 0-禁用 2-禁止新增'" json:"type"`
	Name       string    `gorm:"column:c_name;type:varchar(256)" json:"name"`
	Desc       string    `gorm:"column:c_desc;type:varchar(256)" json:"desc"`
	ModifyTime time.Time `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime time.Time `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator   string    `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	Index      int       `gorm:"column:c_index;type:int(11)" json:"index"`
}

// TableName 指定表名
func (ModuleType) TableName() string {
	return "t_module_type"
}

// ModuleInputType 模块输入类型表结构
type ModuleInputType struct {
	ID                          int    `gorm:"column:c_id;type:int(20);not null" json:"id"`
	ModifyTime                  string `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime                  string `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator                    string `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	InputDataType               int    `gorm:"column:c_input_data_type;type:int(10)" json:"input_data_type"`
	ModuleTypeID                int    `gorm:"column:c_module_type_id;type:int(10);not null;primaryKey" json:"module_type_id"`
	AccessType                  int    `gorm:"column:c_access_type;type:int(10)" json:"access_type"`
	AccessCmd                   int    `gorm:"column:c_access_cmd;type:int(10)" json:"access_cmd"`
	AccessData                  string `gorm:"column:c_access_data;type:varchar(128)" json:"access_data"`
	AccessMcall                 string `gorm:"column:c_access_mcall;type:varchar(128)" json:"access_mcall"`
	AccessDagProcessID          string `gorm:"column:c_access_dag_process_id;type:varchar(128);comment:'Dag业务接口接入的参数：processor_id'" json:"access_dag_process_id"`
	AccessGray                  int    `gorm:"column:c_access_gray;type:tinyint(4);not null;default:0;comment:'是否启用路由灰度'" json:"access_gray"`
	AccessResultType            int    `gorm:"column:c_access_result_type;type:int(10)" json:"access_result_type"`
	AccessResultCmd             int    `gorm:"column:c_access_result_cmd;type:int(10)" json:"access_result_cmd"`
	AccessResultData            string `gorm:"column:c_access_result_data;type:varchar(128)" json:"access_result_data"`
	AccessResultMcall           string `gorm:"column:c_access_result_mcall;type:varchar(128)" json:"access_result_mcall"`
	AccessResultDagProcessID    string `gorm:"column:c_access_result_dag_process_id;type:varchar(128);comment:'条件接口的Dag接入的参数'" json:"access_result_dag_process_id"`
	AccessResultGray            int    `gorm:"column:c_access_result_gray;type:tinyint(4);not null;default:0;comment:'条件接口是否启动灰度'" json:"access_result_gray"`
	SetID                       int    `gorm:"column:c_set_id;type:int(11);not null;primaryKey" json:"set_id"`
	AccessStatusType            int    `gorm:"column:c_access_status_type;type:int(11)" json:"access_status_type"`
	AccessStatusCmd             int    `gorm:"column:c_access_status_cmd;type:int(11)" json:"access_status_cmd"`
	AccessStatusData            string `gorm:"column:c_access_status_data;type:varchar(128)" json:"access_status_data"`
	AccessStatusMcall           string `gorm:"column:c_access_status_mcall;type:varchar(128)" json:"access_status_mcall"`
	AccessConsumeType           int    `gorm:"column:c_access_consume_type;type:int(11)" json:"access_consume_type"`
	AccessConsumeProType        int    `gorm:"column:c_access_consume_pro_type;type:int(11);default:0;comment:'条件消耗回调协议：路由方式'" json:"access_consume_pro_type"`
	AccessConsumeProData        string `gorm:"column:c_access_consume_pro_data;type:varchar(128);comment:'条件消耗回调协议：协议类型（jce/http/PB/PBV2）'" json:"access_consume_pro_data"`
	AccessConsumeProDataOnline  string `gorm:"column:c_access_consume_pro_data_online;type:varchar(128);comment:'条件消耗回调协议：正式环境 - 协议数据'" json:"access_consume_pro_data_online"`
	AccessConsumeCmd            int    `gorm:"column:c_access_consume_cmd;type:int(11)" json:"access_consume_cmd"`
	AccessConsumeData           string `gorm:"column:c_access_consume_data;type:varchar(128)" json:"access_consume_data"`
	AccessConsumeMcall          string `gorm:"column:c_access_consume_mcall;type:varchar(128)" json:"access_consume_mcall"`
	AccessConsumeDagProcessID   string `gorm:"column:c_access_consume_dag_process_id;type:varchar(128);comment:'消耗接口的Dag接入的参数'" json:"access_consume_dag_process_id"`
	AccessConsumeGray           int    `gorm:"column:c_access_consume_gray;type:tinyint(4);not null;default:0;comment:'消耗接口是否启动灰度'" json:"access_consume_gray"`
	AccessAddConsumeType        int    `gorm:"column:c_access_add_consume_type;type:int(11)" json:"access_add_consume_type"`
	AccessAddConsumeCmd         int    `gorm:"column:c_access_add_consume_cmd;type:int(11)" json:"access_add_consume_cmd"`
	AccessAddConsumeData        string `gorm:"column:c_access_add_consume_data;type:varchar(128)" json:"access_add_consume_data"`
	AccessAddConsumeMcall       string `gorm:"column:c_access_add_consume_mcall;type:varchar(128)" json:"access_add_consume_mcall"`
	AppID                       int    `gorm:"column:c_app_id;type:int(11)" json:"app_id"`
	CNName                      string `gorm:"column:c_cn_name;type:varchar(128)" json:"cn_name"`
	ENName                      string `gorm:"column:c_en_name;type:varchar(128)" json:"en_name"`
	Desc                        string `gorm:"column:c_desc;type:varchar(512)" json:"desc"`
	AccessProType               int    `gorm:"column:c_access_pro_type;type:int(11)" json:"access_pro_type"`
	AccessProData               string `gorm:"column:c_access_pro_data;type:varchar(256)" json:"access_pro_data"`
	AccessResultProType         int    `gorm:"column:c_access_result_pro_type;type:int(11)" json:"access_result_pro_type"`
	AccessResultProData         string `gorm:"column:c_access_result_pro_data;type:varchar(256)" json:"access_result_pro_data"`
	AccessDataOnline            string `gorm:"column:c_access_data_online;type:varchar(128)" json:"access_data_online"`
	AccessResultDataOnline      string `gorm:"column:c_access_result_data_online;type:varchar(128)" json:"access_result_data_online"`
	AccessProDataOnline         string `gorm:"column:c_access_pro_data_online;type:varchar(256)" json:"access_pro_data_online"`
	AccessResultProDataOnline   string `gorm:"column:c_access_result_pro_data_online;type:varchar(256)" json:"access_result_pro_data_online"`
	AccessStatusDataOnline      string `gorm:"column:c_access_status_data_online;type:varchar(128)" json:"access_status_data_online"`
	AccessConsumeDataOnline     string `gorm:"column:c_access_consume_data_online;type:varchar(128)" json:"access_consume_data_online"`
	AccessAddConsumeDataOnline  string `gorm:"column:c_access_add_consume_data_online;type:varchar(128)" json:"access_add_consume_data_online"`
	AccessDataQPS               int    `gorm:"column:c_access_data_qps;type:int(11);default:0" json:"access_data_qps"`
	AccessResultProDataQPS      int    `gorm:"column:c_access_result_pro_data_qps;type:int(11);default:0" json:"access_result_pro_data_qps"`
	AccessStatusDataQPS         int    `gorm:"column:c_access_status_data_qps;type:int(11);default:0" json:"access_status_data_qps"`
	AccessConsumeDataQPS        int    `gorm:"column:c_access_consume_data_qps;type:int(11);default:0" json:"access_consume_data_qps"`
	AccessAddConsumeDataQPS     int    `gorm:"column:c_access_add_consume_data_qps;type:int(11);default:0" json:"access_add_consume_data_qps"`
	AccessDataTimeout           int    `gorm:"column:c_access_data_timeout;type:int(11);comment:'业务处理超时时间'" json:"access_data_timeout"`
	AccessResultProDataTimeout  int    `gorm:"column:c_access_result_pro_data_timeout;type:int(11);comment:'条件超时时间'" json:"access_result_pro_data_timeout"`
	AccessStatusDataTimeout     int    `gorm:"column:c_access_status_data_timeout;type:int(11);comment:'消耗状态查询超时时间'" json:"access_status_data_timeout"`
	AccessConsumeDataTimeout    int    `gorm:"column:c_access_consume_data_timeout;type:int(11);comment:'发起消耗超时时间'" json:"access_consume_data_timeout"`
	AccessAddConsumeDataTimeout int    `gorm:"column:c_access_add_consume_data_timeout;type:int(11);comment:'消耗数量增加超时时间'" json:"access_add_consume_data_timeout"`
	ModuleUserType              int    `gorm:"column:c_module_user_type;type:int(11);comment:'模块关联用户类型'" json:"module_user_type"`
	ModuleUserAppID             string `gorm:"column:c_module_user_appid;type:varchar(64);comment:'模块关联appid'" json:"module_user_appid"`
	ModuleUserAppIDWx           string `gorm:"column:c_module_user_appid_wx;type:varchar(64);comment:'wx账号关联用户appid'" json:"module_user_appid_wx"`
	LogID                       int    `gorm:"column:c_log_id;type:int(16);comment:'鹰眼id'" json:"log_id"`
}

// TableName 指定表名
func (ModuleInputType) TableName() string {
	return "t_module_input_type"
}

// ModuleOutputType 模块输出类型表结构
type ModuleOutputType struct {
	ID           int    `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	ModifyTime   string `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime   string `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator     string `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	OutputTypeID int    `gorm:"column:c_output_type_id;type:int(10)" json:"output_type_id"`
	ModuleTypeID int    `gorm:"column:c_module_type_id;type:int(10)" json:"module_type_id"`
}

// TableName 指定表名
func (ModuleOutputType) TableName() string {
	return "t_module_output_type"
}

// ModuleOpType 模块操作类型表结构
type ModuleOpType struct {
	ID                   int    `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	ModifyTime           string `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime           string `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator             string `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	OpTypeID             int    `gorm:"column:c_op_type_id;type:int(10)" json:"op_type_id"`
	Name                 string `gorm:"column:c_name;type:varchar(256)" json:"name"`
	ModuleTypeID         int    `gorm:"column:c_module_type_id;type:int(10)" json:"module_type_id"`
	Desc                 string `gorm:"column:c_desc;type:varchar(256)" json:"desc"`
	SecurityPolicyID     string `gorm:"column:c_security_policy_id;type:varchar(128)" json:"security_policy_id"`
	QPS                  int    `gorm:"column:c_qps;type:int(11);default:0" json:"qps"`
	Timeout              int    `gorm:"column:c_timeout;type:int(11);comment:'超时时间'" json:"timeout"`
	SecurityLevel        int    `gorm:"column:c_security_level;type:int(11);default:2;comment:'安全等级'" json:"security_level"`
	NeedAccount          int    `gorm:"column:c_need_account;type:int(11);default:1;comment:'是否账号无关'" json:"need_account"`
	BlockFailedCondition int    `gorm:"column:c_block_failed_condition;type:int(11);default:0;comment:'是否在框架拦截不符合规则的用户'" json:"block_failed_condition"`
	NeedCache            int    `gorm:"column:c_need_cache;type:int(11);default:0;comment:'是否需要框架缓存结果'" json:"need_cache"`
	CacheKeys            string `gorm:"column:c_cache_keys;type:varchar(512);default:'';comment:'缓存的key，英文逗号隔开'" json:"cache_keys"`
	NeedCondition        int    `gorm:"column:c_need_condition;type:int(11);default:1;comment:'是否需要查询条件'" json:"need_condition"`
	NeedPrivateCondition int    `gorm:"column:c_need_private_condition;type:int(11);default:0;comment:'需要配置独立条件'" json:"need_private_condition"`
	Type                 int    `gorm:"column:c_type;type:int(11);default:0;comment:'接口类型，0活动类，1功能类'" json:"type"`
	ConsumeOpType        int64  `gorm:"column:c_consume_op_type;type:bigint(20);comment:'是否为扣减类动作, 非0表示为扣减类动作，0为非扣减动作'" json:"consume_op_type"`
	AccessValidateTime   int    `gorm:"column:c_access_validate_time;type:int(11);not null;default:0;comment:'是否接入层校验时间'" json:"access_validate_time"`
	SecurityBusinessKey  string `gorm:"column:c_security_business_key;type:varchar(128);not null;default:'';comment:'安全平台鉴权key'" json:"security_business_key"`
	AccountAuthType      int    `gorm:"column:c_account_auth_type;type:int(11);not null;default:0;comment:'账号登陆态校验类型 (0: 校验账号登陆态并透传 (默认); 1: 校验账号登陆态并执行拦截; 2: 不校验也不获取账号登陆态信息)'" json:"account_auth_type"`
	WhitelistCheckType   int    `gorm:"column:c_whitelist_check_type;type:int(11);not null;default:0;comment:'白名单检查类型（0: 预发布环境下必须是白名单用户才可访问 (默认); 1: 预发布环境下允许任何用户访问）'" json:"whitelist_check_type"`
	SecurityCheckType    int    `gorm:"column:c_security_check_type;type:int(11);not null;default:0;comment:'安全检查类型（0: 安全检查允许超时, 超时视同放过 (默认); 1: 安全检查不允许超时, 超时视同安全检查不通过）'" json:"security_check_type"`
}

// TableName 指定表名
func (ModuleOpType) TableName() string {
	return "t_module_op_type"
}

// AdminConfigV3 管理配置V3表结构
type AdminConfigV3 struct {
	ID         string `gorm:"column:c_id;type:varchar(128);not null;primaryKey;comment:'配置id'" json:"id"`
	Config     string `gorm:"column:c_config;type:mediumtext;comment:'模块扩展配置'" json:"config"`
	Time       string `gorm:"column:c_time;type:datetime" json:"time"`
	ConfigType string `gorm:"column:c_type;type:varchar(255);not null;primaryKey" json:"config_type"`
}

// TableName 指定表名
func (AdminConfigV3) TableName() string {
	return "t_admin_config_v3"
}

// OutputType 输出类型表结构
type OutputType struct {
	ID         int            `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	Name       string         `gorm:"column:c_name;type:varchar(256)" json:"name"`
	Desc       string         `gorm:"column:c_desc;type:varchar(256)" json:"desc"`
	ModifyTime time.Time      `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime time.Time      `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	DataType   OutputDataType `gorm:"column:c_data_type;type:int(10)" json:"data_type"`
	Operator   string         `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	FailedMsg  string         `gorm:"column:c_failed_msg;type:varchar(256)" json:"failed_msg"`
	ENName     string         `gorm:"column:c_en_name;type:varchar(128)" json:"en_name"`
	DataUIType string         `gorm:"column:c_data_ui_type;type:varchar(10);comment:'展示的UI组件'" json:"data_ui_type"`
}

// TableName 指定表名
func (OutputType) TableName() string {
	return "t_output_type"
}

// Condition 条件表结构
type Condition struct {
	ID          int       `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	Result      int       `gorm:"column:c_result;type:int(10);not null" json:"result"`
	Name        string    `gorm:"column:c_name;type:varchar(512);not null" json:"name"`
	ModifyTime  time.Time `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime  time.Time `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator    string    `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	ModuleID    int       `gorm:"column:c_module_id;type:int(10)" json:"module_id"`
	SourceType  int       `gorm:"column:c_source_type;type:int(10)" json:"source_type"`
	ScoreResult string    `gorm:"column:c_score_result;type:varchar(255)" json:"score_result"`
	Priority    int       `gorm:"column:c_prioprity;type:int(11)" json:"priority"`
	Option      int       `gorm:"column:c_option;type:int(11);default:0;comment:'模块动作, 历史字段，固定为0'" json:"option"`
	CondName    string    `gorm:"column:c_cond_name;type:varchar(50);default:'';comment:'条件组名称'" json:"cond_name"`
}

// TableName 指定表名
func (Condition) TableName() string {
	return "t_condition"
}

// ConditionItem 条件项表结构
type ConditionItem struct {
	ID                int       `gorm:"column:c_id;type:int(20);primaryKey;autoIncrement" json:"id"`
	OutputType        int       `gorm:"column:c_output_type;type:int(10) unsigned;not null" json:"output_type"`
	Value             int       `gorm:"column:c_value;type:int(10);not null" json:"value"`
	Op                int       `gorm:"column:c_op;type:int(10);not null" json:"op"`
	ModifyTime        time.Time `gorm:"column:c_m_time;type:timestamp;not null;default:CURRENT_TIMESTAMP" json:"modify_time"`
	CreateTime        time.Time `gorm:"column:c_c_time;type:timestamp;not null;default:'0000-00-00 00:00:00'" json:"create_time"`
	Operator          string    `gorm:"column:c_operator;type:varchar(128)" json:"operator"`
	RelateModuleID    int       `gorm:"column:c_relate_module_id;type:int(10)" json:"relate_module_id"`
	ModuleID          int       `gorm:"column:c_module_id;type:int(10)" json:"module_id"`
	ConditionID       int       `gorm:"column:c_condition_id;type:int(10)" json:"condition_id"`
	IsSelect          int       `gorm:"column:c_is_select;type:int(10)" json:"is_select"`
	IsRelateOutput    int       `gorm:"column:c_is_relate_output;type:int(10)" json:"is_relate_output"`
	FailedMsg         string    `gorm:"column:c_failed_msg;type:varchar(256)" json:"failed_msg"`
	Sort              int       `gorm:"column:c_sort;type:int(10) unsigned" json:"sort"`
	ConsumeNumPerTime int       `gorm:"column:c_consume_num_per_time;type:int(11)" json:"consume_num_per_time"`
}

// TableName 指定表名
func (ConditionItem) TableName() string {
	return "t_condition_item"
}

// ModuleSecurity 模块安全策略表结构
type ModuleSecurity struct {
	ModType      int32  `gorm:"column:c_mod_type;type:int(11);primaryKey;autoIncrement" json:"mod_type"`
	SecurityID   string `gorm:"column:c_security_id;type:text" json:"security_id"`
	SecurityDesc string `gorm:"column:c_security_desc;type:text" json:"security_desc"`
}

// TableName specifies the table name
func (ModuleSecurity) TableName() string {
	return "t_module_security"
}

// CompleteModuleTypeInfo 完整的模块类型信息（包含关联的操作类型、输出类型和UI schema）
type CompleteModuleTypeInfo struct {
	ModuleType    *ModuleType      `json:"module_type"`
	ModuleOpTypes []*ModuleOpType  `json:"module_op_types"`
	OutputTypes   []*OutputType    `json:"output_types"`
	InputTypes    *ModuleInputType `json:"input_types"`
	UISchema      *AdminConfigV3   `json:"ui_schema"`
}
