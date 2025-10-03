import { Pool } from "pg";
import logger from "./logger";

const pool = new Pool({
  connectionString: process.env.DATABASE_URL,
  max: 20,
  idleTimeoutMillis: 30000,
  connectionTimeoutMillis: 2000,
});

export interface User {
  id: string;
  name?: string | null;
  email: string;
  emailVerified?: Date | null;
  image?: string | null;
  hashedPassword?: string | null;
  createdAt: Date;
  updatedAt: Date;
}

export async function getUserTenant(userId: string): Promise<User | null> {
  try {
    const result = await pool.query(
      'SELECT id, name, email, "emailVerified", image, hashed_password as "hashedPassword", "createdAt", "updatedAt" FROM users WHERE id = $1',
      [userId]
    );
    return result.rows[0] || null;
  } catch (error) {
    logger.error("Error fetching user tenant", { error, userId });
    return null;
  }
}

export async function createUser(userData: {
  email: string;
  name?: string;
  hashedPassword?: string;
}): Promise<User> {
  try {
    const result = await pool.query(
      `INSERT INTO users (email, name, hashed_password, "createdAt", "updatedAt")
       VALUES ($1, $2, $3, NOW(), NOW())
       RETURNING id, name, email, "emailVerified", image, hashed_password as "hashedPassword", "createdAt", "updatedAt"`,
      [userData.email, userData.name || null, userData.hashedPassword || null]
    );
    const newUser = result.rows[0];
    logger.info("User created successfully", {
      userId: newUser.id,
      email: userData.email,
    });
    return newUser;
  } catch (error) {
    logger.error("Error creating user", { error, email: userData.email });
    throw error;
  }
}

export async function getUserByEmail(email: string): Promise<User | null> {
  try {
    const result = await pool.query(
      'SELECT id, name, email, "emailVerified", image, hashed_password as "hashedPassword", "createdAt", "updatedAt" FROM users WHERE email = $1',
      [email]
    );
    return result.rows[0] || null;
  } catch (error) {
    logger.error("Error fetching user by email", { error, email });
    return null;
  }
}

export default pool;
