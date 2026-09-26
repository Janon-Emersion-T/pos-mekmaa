-- Each staff member has one open register session; other staff may open theirs.
DROP INDEX one_open_register_session;
CREATE UNIQUE INDEX one_open_register_session_per_user ON register_sessions(opened_by) WHERE status='open';
