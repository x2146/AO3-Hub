import { Link } from "@tanstack/react-router";
import { BookOpenText, Home } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

export function NotFound() {
  return (
    <Empty className="min-h-[calc(100svh-14rem)] border border-dashed">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <BookOpenText />
        </EmptyMedia>
        <EmptyTitle>页面没有找到</EmptyTitle>
        <EmptyDescription>
          这里没有你要找的故事，链接可能已经失效。
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <Link to="/">
            <Home data-icon="inline-start" />
            返回书架
          </Link>
        </Button>
      </EmptyContent>
    </Empty>
  );
}
