import Link from "next/link";
import { Button } from "@/components/ui/Button";

export default function Home() {
  return (
    <div className="min-h-screen bg-gray-50 flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8">
      <div className="max-w-md w-full space-y-8">
        <div className="text-center">
          <h1 className="text-4xl font-bold text-gray-900 mb-4">
            Outpost Dashboard Integration Demo
          </h1>
          <p className="text-lg text-gray-600 mb-8">
            Experience seamless integration between authentication and Outpost
            event destinations management.
          </p>

          <div className="space-y-6">
            <Link href="/auth/login" className="block">
              <Button className="w-full" size="lg">
                Sign In
              </Button>
            </Link>

            <Link href="/auth/register" className="block">
              <Button variant="outline" className="w-full" size="lg">
                Create Account
              </Button>
            </Link>
          </div>

          <div className="mt-8 text-sm text-gray-500">
            <p className="mb-4">This demo showcases:</p>
            <ul className="text-left space-y-2">
              <li>
                • User registration with automatic Outpost tenant creation
              </li>
              <li>• Dashboard overview with tenant statistics</li>
              <li>
                • Seamless redirect to Outpost portal for destination management
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}
