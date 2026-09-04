import Link from "next/link";
import { LogOut } from "lucide-react";
import { requireAdmin } from "@/lib/auth";
import { logoutAction } from "./actions";
import Brand from "@/components/Brand";
import { MobileNav, SidebarNav } from "@/components/DashboardNav";
import PageTransition from "@/components/motion/PageTransition";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";

export default async function AdminLayout({ children }) {
  const admin = await requireAdmin();

  return (
    <div className="min-h-screen">
      <div className="flex min-h-screen">
        <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-r border-white/5 bg-[#0a0a0a]/50 p-4 backdrop-blur-2xl lg:flex">
          <Link
            href="/dashboard"
            className="group flex items-center justify-center px-3"
          >
            <Brand
              showText={true}
              width={32}
              height={32}
              className="transition-transform duration-300 group-hover:scale-105"
            />
          </Link>
          <Separator className="my-4" />
          <SidebarNav />
          <div className="mt-auto">
            <Separator className="mb-4" />
            <p className="truncate px-3 pb-3 text-xs text-neutral-500">
              Signed in as {admin.username}
            </p>
            <form action={logoutAction}>
              <Button
                className="w-full justify-start"
                variant="ghost"
                type="submit"
              >
                <LogOut className="size-4" />
                Sign out
              </Button>
            </form>
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="sticky top-0 z-10 flex items-center gap-2 overflow-x-auto border-b border-white/5 bg-[#0a0a0a]/60 px-4 py-3 backdrop-blur-2xl lg:hidden">
            <MobileNav />
          </header>
          <main className="flex-1 p-5 sm:p-8 lg:p-10">
            <div className="mx-auto max-w-7xl">
              <PageTransition>{children}</PageTransition>
            </div>
          </main>
        </div>
      </div>
    </div>
  );
}
