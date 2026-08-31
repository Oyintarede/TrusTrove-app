import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ProfilePage from "@/app/profile/page";

vi.mock("@/store/wallet", () => ({
  useWalletStore: (selector: any) => {
    const state = { connected: true, address: "GTESTADDRESS" };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@/hooks/useProfile", () => ({
  useProfile: () => ({
    profile: null,
    isProfileLoading: false,
    isVerified: false,
    isVerifiedLoading: false,
    register: vi.fn(),
    isRegistering: false,
    registerError: null,
  }),
}));

vi.mock("@/components/shared/PageLayout", () => ({
  PageLayout: ({ children }: { children: React.ReactNode }) => (
    <main>{children}</main>
  ),
}));

vi.mock("@/components/shared/ErrorBoundary", () => ({
  ErrorBoundary: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@/components/shared/WalletConnect", () => ({
  WalletConnect: () => null,
}));

vi.mock("@/components/shared/TransactionPending", () => ({
  TransactionPending: () => null,
}));

describe("Profile registration dialog", () => {
  it("opens in an accessible fixed overlay", () => {
    render(<ProfilePage />);

    fireEvent.click(screen.getByRole("button", { name: "Register profile" }));

    const dialog = screen.getByRole("dialog", {
      name: "Register Business Metadata",
    });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAttribute("tabindex", "-1");
    expect(dialog).toHaveClass("fixed", "inset-0", "z-50");
    expect(
      screen.getByRole("button", { name: "Close registration dialog" }),
    ).toBeInTheDocument();
  });
});
