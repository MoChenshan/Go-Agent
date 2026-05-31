-- StoreFeedback 存储用户反馈记录
INSERT INTO t_oncall_agent_feedback 
    (sessionID, userID, sessionhistory, isPositive) 
    VALUES (?, ?, ?, ?)
