import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getOutpostClient } from "@/lib/outpost";
import logger from "@/lib/logger";

export async function GET(request: NextRequest) {
  try {
    // Check authentication using JWT token (same pattern as other routes)
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const tenantId = token.id as string; // Use user ID as tenant ID

    logger.info("Fetching topics for tenant", { tenantId });

    // Get Outpost client
    let outpostClient;
    try {
      outpostClient = getOutpostClient();
    } catch (error) {
      logger.error("Failed to initialize Outpost client:", error);
      return NextResponse.json(
        { error: "Failed to initialize service" },
        { status: 500 }
      );
    }

    try {
      // Fetch topics using the Outpost SDK
      const topics = await outpostClient.topics.list({ tenantId });

      logger.info("Topics fetched successfully", {
        tenantId,
        topicsCount: topics?.length || 0,
      });

      return NextResponse.json(topics || []);
    } catch (outpostError: any) {
      logger.error("Failed to fetch topics via Outpost:", {
        error: outpostError,
        tenantId,
      });

      // Handle specific Outpost SDK errors
      if (outpostError?.response?.status === 404) {
        return NextResponse.json(
          { error: "Tenant not found" },
          { status: 404 }
        );
      }

      if (outpostError?.response?.status === 403) {
        return NextResponse.json(
          { error: "Access denied" },
          { status: 403 }
        );
      }

      return NextResponse.json(
        {
          error: "Failed to fetch topics",
          details: outpostError?.message || "Unknown error occurred",
        },
        { status: 500 }
      );
    }
  } catch (error: any) {
    logger.error("Unexpected error in topics API:", {
      error,
      errorMessage: error?.message,
    });

    return NextResponse.json(
      {
        error: "Internal server error",
        details:
          process.env.NODE_ENV === "development" ? error?.message : undefined,
      },
      { status: 500 }
    );
  }
}

// Only allow GET method - return 405 for other methods
export async function POST() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "GET" } }
  );
}

export async function PUT() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "GET" } }
  );
}

export async function DELETE() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "GET" } }
  );
}

export async function PATCH() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "GET" } }
  );
}