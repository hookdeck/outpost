import { NextRequest, NextResponse } from "next/server";
import bcrypt from "bcryptjs";
import { createUser, getUserByEmail } from "../../../../lib/db";
import { createTenant } from "../../../../lib/outpost";
import { generateTenantId } from "../../../../lib/utils";
import { registerSchema } from "../../../../lib/validations";
import logger from "../../../../lib/logger";

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const validatedData = registerSchema.parse(body);

    // Check if user already exists
    const existingUser = await getUserByEmail(validatedData.email);
    if (existingUser) {
      return NextResponse.json(
        { message: "User already exists" },
        { status: 400 }
      );
    }

    // Hash the password before storing
    const hashedPassword = await bcrypt.hash(validatedData.password, 12);

    // Create user in database with hashed password
    const user = await createUser({
      email: validatedData.email,
      name: validatedData.name,
      hashedPassword,
    });

    // Create tenant in Outpost using the user ID
    const tenantId = generateTenantId(user.id.toString());
    logger.info(`Creating Outpost tenant for new user registration`, {
      userId: user.id,
      tenantId,
      email: validatedData.email,
    });

    try {
      await createTenant(tenantId);
      logger.info(
        `User registration completed successfully with Outpost tenant`,
        {
          userId: user.id,
          tenantId,
          email: validatedData.email,
        }
      );
    } catch (outpostError) {
      logger.error("Failed to create Outpost tenant during registration", {
        error: outpostError,
        tenantId,
        userId: user.id,
        email: validatedData.email,
      });
      // Continue with registration even if tenant creation fails
      // In production, you might want to retry or handle this differently
    }

    return NextResponse.json(
      { message: "User created successfully" },
      { status: 201 }
    );
  } catch (error: any) {
    logger.error("Registration error", { error, errorMessage: error?.message });

    if (error.errors) {
      // Validation error
      return NextResponse.json(
        { message: "Validation failed", errors: error.errors },
        { status: 400 }
      );
    }

    return NextResponse.json(
      { message: "Internal server error" },
      { status: 500 }
    );
  }
}
