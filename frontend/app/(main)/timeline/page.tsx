"use client";

import { useEffect, useState } from "react";
import api from "@/lib/api";
import { useDataCache } from "@/stores/data-cache";
import { useReminderPolling } from "@/hooks/use-reminder-polling";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import {
  CalendarDays,
  Plus,
  Trash2,
  Clock,
  Users,
  FolderOpen,
  CalendarX,
} from "lucide-react";

interface Team {
  id: string;
  name: string;
}

interface Project {
  id: string;
  name: string;
}

interface TimelineEvent {
  id: string;
  title: string;
  description: string;
  start_time: string;
  end_time: string | null;
  is_team_event: boolean;
  reminder_enabled: boolean;
  reminder_minutes_before: number | null;
}

export default function TimelinePage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeam, setSelectedTeam] = useState<string>("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [startTime, setStartTime] = useState("");
  const [endTime, setEndTime] = useState("");
  const [isTeamEvent, setIsTeamEvent] = useState(false);
  const [reminderEnabled, setReminderEnabled] = useState(false);
  const [reminderMinutesBefore, setReminderMinutesBefore] = useState(15);
  const [loadingTeams, setLoadingTeams] = useState(true);
  const [loadingEvents, setLoadingEvents] = useState(false);
  const dataCache = useDataCache();

  const [addDialogOpen, setAddDialogOpen] = useState(false);

  useEffect(() => {
    fetchTeams();
  }, []);

  useEffect(() => {
    if (selectedTeam) {
      fetchProjects(selectedTeam);
    }
  }, [selectedTeam]);

  useEffect(() => {
    if (selectedProject) {
      fetchEvents(selectedProject);
    }
  }, [selectedProject]);

  useReminderPolling(selectedProject);

  const fetchTeams = async () => {
    const cached = dataCache.getTeams();
    if (cached && !dataCache.isTeamsStale()) {
      setTeams(cached);
      if (cached.length > 0 && !selectedTeam) {
        setSelectedTeam(cached[0].id);
      }
      setLoadingTeams(false);
      return;
    }
    setLoadingTeams(true);
    try {
      const res = await api.get("/teams");
      setTeams(res.data);
      dataCache.setTeams(res.data);
      if (res.data.length > 0 && !selectedTeam) {
        setSelectedTeam(res.data[0].id);
      }
    } catch {
      setTeams([]);
    } finally {
      setLoadingTeams(false);
    }
  };

  const fetchProjects = async (teamId: string) => {
    const cached = dataCache.getProjects(teamId);
    if (cached && !dataCache.isProjectsStale(teamId)) {
      setProjects(cached);
      if (cached.length > 0) {
        setSelectedProject(cached[0].id);
      } else {
        setSelectedProject("");
        setEvents([]);
      }
      return;
    }
    try {
      const res = await api.get("/projects", { params: { team_id: teamId } });
      setProjects(res.data);
      dataCache.setProjects(teamId, res.data);
      if (res.data.length > 0) {
        setSelectedProject(res.data[0].id);
      } else {
        setSelectedProject("");
        setEvents([]);
      }
    } catch {
      setProjects([]);
      setSelectedProject("");
      setEvents([]);
    }
  };

  const fetchEvents = async (projectId: string) => {
    setLoadingEvents(true);
    try {
      const res = await api.get(`/timeline/${projectId}/events`);
      setEvents(res.data);
    } catch {
      setEvents([]);
    } finally {
      setLoadingEvents(false);
    }
  };

  const createEvent = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject) {
      toast.error("请先选择项目");
      return;
    }
    try {
      await api.post(`/timeline/${selectedProject}/events`, null, {
        params: {
          title,
          description,
          start_time: startTime,
          end_time: endTime || undefined,
          is_team_event: isTeamEvent,
          reminder_enabled: reminderEnabled,
          reminder_minutes_before: reminderEnabled ? reminderMinutesBefore : undefined,
        },
      });
      setTitle("");
      setDescription("");
      setStartTime("");
      setEndTime("");
      setIsTeamEvent(false);
      setReminderEnabled(false);
      setReminderMinutesBefore(15);
      setAddDialogOpen(false);
      fetchEvents(selectedProject);
      toast.success("日程添加成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "添加失败");
    }
  };

  const deleteEvent = async (eventId: string) => {
    if (!selectedProject) return;
    try {
      await api.delete(`/timeline/${selectedProject}/events/${eventId}`);
      fetchEvents(selectedProject);
      toast.success("日程已删除");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "删除失败");
    }
  };

  const formatDate = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleString("zh-CN", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const formatFullDate = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleString("zh-CN");
  };

  return (
    <>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">时间线</h1>
          <p className="text-sm text-muted-foreground">
            管理项目日程和里程碑
          </p>
        </div>
        <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
          <DialogTrigger asChild>
            <Button disabled={projects.length === 0}>
              <Plus className="h-4 w-4 mr-1" />
              添加日程
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>添加日程</DialogTitle>
              <DialogDescription>
                为当前项目添加一个新的日程安排
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={createEvent} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="title">标题</Label>
                <Input
                  id="title"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="日程标题"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="description">描述</Label>
                <Input
                  id="description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="可选"
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="startTime">开始时间</Label>
                  <Input
                    id="startTime"
                    type="datetime-local"
                    value={startTime}
                    onChange={(e) => setStartTime(e.target.value)}
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="endTime">结束时间</Label>
                  <Input
                    id="endTime"
                    type="datetime-local"
                    value={endTime}
                    onChange={(e) => setEndTime(e.target.value)}
                  />
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="isTeamEvent"
                  checked={isTeamEvent}
                  onCheckedChange={(checked) =>
                    setIsTeamEvent(checked === true)
                  }
                />
                <Label htmlFor="isTeamEvent">团队日程</Label>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={reminderEnabled}
                  onChange={(e) => setReminderEnabled(e.target.checked)}
                />
                开启提醒
              </label>
              {reminderEnabled && (
                <select
                  value={reminderMinutesBefore}
                  onChange={(e) => setReminderMinutesBefore(Number(e.target.value))}
                  className="border rounded px-2 py-1 text-sm"
                >
                  <option value={5}>5分钟前</option>
                  <option value={15}>15分钟前</option>
                  <option value={30}>30分钟前</option>
                  <option value={60}>1小时前</option>
                  <option value={1440}>1天前</option>
                </select>
              )}
              <DialogFooter>
                <Button type="submit">添加</Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 xl:grid-cols-4 gap-6">
        {/* 左侧：选择器 */}
        <div className="space-y-6">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Users className="h-4 w-4" />
                选择团队
              </CardTitle>
            </CardHeader>
            <CardContent>
              {loadingTeams ? (
                <Skeleton className="h-9 w-full" />
              ) : (
                <Select value={selectedTeam} onValueChange={setSelectedTeam}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择团队" />
                  </SelectTrigger>
                  <SelectContent>
                    {teams.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <FolderOpen className="h-4 w-4" />
                选择项目
              </CardTitle>
            </CardHeader>
            <CardContent>
              {projects.length === 0 ? (
                <p className="text-sm text-muted-foreground">暂无项目</p>
              ) : (
                <Select
                  value={selectedProject}
                  onValueChange={setSelectedProject}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="选择项目" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </CardContent>
          </Card>
        </div>

        {/* 右侧：日程表 */}
        <div className="lg:col-span-2 xl:col-span-3">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <CalendarDays className="h-5 w-5" />
                日程表
              </CardTitle>
              <CardDescription>
                {events.length} 个日程安排
              </CardDescription>
            </CardHeader>
            <CardContent>
              {loadingEvents ? (
                <div className="space-y-3">
                  <Skeleton className="h-20 w-full" />
                  <Skeleton className="h-20 w-full" />
                  <Skeleton className="h-20 w-full" />
                </div>
              ) : events.length === 0 ? (
                <div className="text-center py-12">
                  <CalendarX className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                  <p className="text-muted-foreground">暂无日程</p>
                  <p className="text-sm text-muted-foreground mt-1">
                    点击右上角添加新日程
                  </p>
                </div>
              ) : (
                <div className="space-y-3">
                  {events.map((evt) => (
                    <div
                      key={evt.id}
                      className="flex gap-4 p-4 rounded-lg border hover:bg-muted/50 transition-colors"
                    >
                      <div className="flex flex-col items-center justify-center min-w-[60px]">
                        <div className="text-2xl font-bold text-primary">
                          {new Date(evt.start_time).getDate()}
                        </div>
                        <div className="text-xs text-muted-foreground uppercase">
                          {new Date(evt.start_time).toLocaleString("zh-CN", {
                            month: "short",
                          })}
                        </div>
                      </div>
                      <Separator orientation="vertical" className="h-auto" />
                      <div className="flex-1 min-w-0">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0">
                            <h3 className="font-medium truncate">
                              {evt.title}
                              {evt.reminder_enabled && <span className="text-sm ml-1">🔔</span>}
                            </h3>
                            {evt.description && (
                              <p className="text-sm text-muted-foreground mt-1">
                                {evt.description}
                              </p>
                            )}
                            <div className="flex items-center gap-3 mt-2 text-xs text-muted-foreground">
                              <span className="flex items-center gap-1">
                                <Clock className="h-3 w-3" />
                                {formatFullDate(evt.start_time)}
                                {evt.end_time &&
                                  ` ~ ${formatFullDate(evt.end_time)}`}
                              </span>
                            </div>
                            <div className="mt-2">
                              {evt.is_team_event && (
                                <Badge variant="secondary">团队</Badge>
                              )}
                            </div>
                          </div>
                          <AlertDialog>
                            <AlertDialogTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="shrink-0 text-destructive hover:text-destructive"
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </AlertDialogTrigger>
                            <AlertDialogContent>
                              <AlertDialogHeader>
                                <AlertDialogTitle>确认删除</AlertDialogTitle>
                                <AlertDialogDescription>
                                  确定要删除日程「{evt.title}」吗？此操作不可撤销。
                                </AlertDialogDescription>
                              </AlertDialogHeader>
                              <AlertDialogFooter>
                                <AlertDialogCancel>取消</AlertDialogCancel>
                                <AlertDialogAction
                                  onClick={() => deleteEvent(evt.id)}
                                  className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                >
                                  删除
                                </AlertDialogAction>
                              </AlertDialogFooter>
                            </AlertDialogContent>
                          </AlertDialog>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  );
}
