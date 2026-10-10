-- 0002: repair rows created before users.nickname became required by the API.
UPDATE users SET nickname = username WHERE nickname = '';
