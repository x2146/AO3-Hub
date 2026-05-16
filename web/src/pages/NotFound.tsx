import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";

export function NotFound() {
  return (
    <div className="py-32 text-center space-y-4">
      <p className="text-[44px] font-semibold tracking-tight">404</p>
      <p className="text-muted-foreground">这里没有你要找的故事。</p>
      <Button variant="outline" asChild>
        <Link to="/">返回书架</Link>
      </Button>
    </div>
  );
}
