"use client";

import Link from "next/link";
import { useSession, signOut } from "next-auth/react";
import { Button } from "@/components/ui/Button";

export default function Header() {
  const { data: session } = useSession();

  return (
    <header className="bg-white border-b border-gray-200">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between items-center h-16">
          <div className="flex items-center">
            <Link href="/dashboard" className="text-xl font-bold text-gray-900">
              Back to API Platform Demo
            </Link>
          </div>

          <div className="flex items-center space-x-4">
            {session?.user && (
              <>
                <span className="text-sm text-gray-700">
                  Welcome, {session.user.name || session.user.email}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => signOut({ callbackUrl: "/auth/login" })}
                >
                  Sign out
                </Button>
              </>
            )}
          </div>
        </div>
      </div>
    </header>
  );
}
